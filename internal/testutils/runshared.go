// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package testutils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
)

const (
	runSharedPrefix = "dmrhub-test-"
	runSharedLabel  = "dmrhub-test.run"
	runKeyEnv       = "DMRHUB_TEST_RUN"
)

var joins atomic.Int64 //nolint:gochecknoglobals // numbers the joins of the process

type runSharedSpec struct {
	kind, image, tag string
	env, cmd         []string
	port             string
}

// runKey returns the ID of the go test run of the process: the PID of the
// go command, which starts the test binary of each package, or the value
// of DMRHUB_TEST_RUN.
func runKey() int {
	if v, err := strconv.Atoi(os.Getenv(runKeyEnv)); err == nil && v > 0 {
		return v
	}
	return os.Getppid()
}

func runSharedName(kind string, run int) string {
	return fmt.Sprintf("%s%s-%d", runSharedPrefix, kind, run)
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func newDockerClient() (*mobyclient.Client, error) {
	cli, err := mobyclient.New(mobyclient.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return cli, nil
}

func runSharedDir(run int) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s%d", runSharedPrefix, run))
}

func withRunLock(run int, fn func(dir string) error) error {
	dir := runSharedDir(run)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("make the registry of run %d: %w", run, err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open the lock of run %d: %w", run, err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock the registry of run %d: %w", run, err)
	}
	return fn(dir)
}

func reapStaleRuns(ctx context.Context, cli *mobyclient.Client) error {
	list, err := cli.ContainerList(ctx, mobyclient.ContainerListOptions{All: true, Filters: mobyclient.Filters{}.Add("label", runSharedLabel)})
	if err != nil {
		return fmt.Errorf("list the shared servers: %w", err)
	}
	var errs []error
	for _, c := range list.Items {
		run, err := strconv.Atoi(c.Labels[runSharedLabel])
		if err != nil || run <= 0 || processAlive(run) {
			continue
		}
		if !slices.ContainsFunc(c.Names, func(n string) bool {
			return strings.HasPrefix(strings.TrimPrefix(n, "/"), runSharedPrefix) && strings.HasSuffix(n, "-"+strconv.Itoa(run))
		}) {
			continue
		}
		if err := removeStale(ctx, cli, c.ID); err != nil {
			errs = append(errs, err)
		}
		_ = os.RemoveAll(runSharedDir(run))
	}
	reapStaleDirs()
	return errors.Join(errs...)
}

func reapStaleDirs() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), runSharedPrefix)
		if !ok || !e.IsDir() {
			continue
		}
		if run, err := strconv.Atoi(rest); err == nil && run > 0 && !processAlive(run) {
			_ = os.RemoveAll(filepath.Join(os.TempDir(), e.Name()))
		}
	}
}

// staleRemover is the part of the Docker client that removes a stale
// server.
type staleRemover interface {
	ContainerRemove(ctx context.Context, id string, options mobyclient.ContainerRemoveOptions) (mobyclient.ContainerRemoveResult, error)
	ContainerInspect(ctx context.Context, id string, options mobyclient.ContainerInspectOptions) (mobyclient.ContainerInspectResult, error)
}

// removeStale removes the stale server id. A server that another run
// removes already, which Docker answers with "removal of container ... is
// already in progress" before it shows the container as removing, or that
// is gone, counts as removed.
func removeStale(ctx context.Context, cli staleRemover, id string) error {
	_, err := cli.ContainerRemove(ctx, id, mobyclient.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	if err == nil || cerrdefs.IsNotFound(err) || strings.Contains(err.Error(), "is already in progress") || !stillThere(ctx, cli, id) {
		return nil
	}
	return fmt.Errorf("remove the stale server %s: %w", id, err)
}

func stillThere(ctx context.Context, cli staleRemover, id string) bool {
	res, err := cli.ContainerInspect(ctx, id, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return false
	}
	return res.Container.State == nil || res.Container.State.Status != container.StateRemoving
}

func findShared(ctx context.Context, cli *mobyclient.Client, name string) (string, bool, error) {
	list, err := cli.ContainerList(ctx, mobyclient.ContainerListOptions{All: true, Filters: mobyclient.Filters{}.Add("name", "^/"+name+"$")})
	if err != nil {
		return "", false, fmt.Errorf("find %s: %w", name, err)
	}
	if len(list.Items) == 0 {
		return "", false, nil
	}
	return list.Items[0].ID, true, nil
}

// joinShared returns the ID of the server of spec that the processes of
// the run of the caller share, starting it when no process of the run has,
// and a func that leaves it. The last process of the run to leave removes
// the server. It does not wait for the server to be ready.
func joinShared(ctx context.Context, cli *mobyclient.Client, spec runSharedSpec) (string, func(context.Context) error, error) {
	if err := reapStaleRuns(ctx, cli); err != nil {
		return "", nil, err
	}
	run := runKey()
	name := runSharedName(spec.kind, run)
	member := fmt.Sprintf("%s.%d.%d", spec.kind, os.Getpid(), joins.Add(1))
	var id string
	err := withRunLock(run, func(dir string) error {
		if err := os.WriteFile(filepath.Join(dir, member), nil, 0o644); err != nil {
			return fmt.Errorf("register in run %d: %w", run, err)
		}
		found, ok, err := findShared(ctx, cli, name)
		if err != nil {
			return err
		}
		if ok {
			id = found
			return nil
		}
		id, err = startShared(ctx, cli, spec, name, run)
		return err
	})
	if err != nil {
		return "", nil, err
	}
	leave := func(ctx context.Context) error {
		return withRunLock(run, func(dir string) error {
			_ = os.Remove(filepath.Join(dir, member))
			if othersIn(dir, spec.kind) {
				return nil
			}
			if _, err := cli.ContainerRemove(ctx, id, mobyclient.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
				return fmt.Errorf("remove %s: %w", name, err)
			}
			return nil
		})
	}
	return id, leave, nil
}

func othersIn(dir, kind string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), kind+".")
		if !ok {
			continue
		}
		pidText, _, _ := strings.Cut(rest, ".")
		pid, err := strconv.Atoi(pidText)
		if err != nil {
			continue
		}
		if processAlive(pid) {
			return true
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	return false
}

func startShared(ctx context.Context, cli *mobyclient.Client, spec runSharedSpec, name string, run int) (string, error) {
	ref := spec.image + ":" + spec.tag
	if _, err := cli.ImageInspect(ctx, ref); err != nil {
		pull, err := cli.ImagePull(ctx, ref, mobyclient.ImagePullOptions{})
		if err != nil {
			return "", fmt.Errorf("pull %s: %w", ref, err)
		}
		_, _ = io.Copy(io.Discard, pull)
		_ = pull.Close()
	}
	port, err := network.ParsePort(spec.port)
	if err != nil {
		return "", fmt.Errorf("port %s: %w", spec.port, err)
	}
	created, err := cli.ContainerCreate(ctx, mobyclient.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image:        ref,
			Env:          spec.env,
			Cmd:          spec.cmd,
			Labels:       map[string]string{runSharedLabel: strconv.Itoa(run)},
			ExposedPorts: network.PortSet{port: struct{}{}},
		},
		HostConfig: &container.HostConfig{
			PublishAllPorts: true,
			RestartPolicy:   container.RestartPolicy{Name: container.RestartPolicyDisabled},
			Resources:       container.Resources{NanoCPUs: 2e9},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := cli.ContainerStart(ctx, created.ID, mobyclient.ContainerStartOptions{}); err != nil {
		_, _ = cli.ContainerRemove(context.WithoutCancel(ctx), created.ID, mobyclient.ContainerRemoveOptions{Force: true})
		return "", fmt.Errorf("start %s: %w", name, err)
	}
	return created.ID, nil
}

func sharedPort(ctx context.Context, cli *mobyclient.Client, id, portID string) (int, error) {
	res, err := cli.ContainerInspect(ctx, id, mobyclient.ContainerInspectOptions{})
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", id, err)
	}
	if res.Container.State == nil {
		return 0, nil
	}
	if !res.Container.State.Running {
		return 0, fmt.Errorf("server %s is %s", id, res.Container.State.Status)
	}
	port, err := network.ParsePort(portID)
	if err != nil {
		return 0, fmt.Errorf("port %s: %w", portID, err)
	}
	if res.Container.NetworkSettings == nil {
		return 0, nil
	}
	for _, b := range res.Container.NetworkSettings.Ports[port] {
		if p, err := strconv.Atoi(b.HostPort); err == nil {
			return p, nil
		}
	}
	return 0, nil
}
