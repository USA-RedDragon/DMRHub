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
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	mobyclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sharedHelperEnv = "DMRHUB_TEST_SHARED_HELPER"

var probeSpec = runSharedSpec{kind: "redis-probe", image: "redis", tag: "7-alpine", port: "6379/tcp"} //nolint:gochecknoglobals // the server of the tests of the run-shared servers

// TestRunSharedHelper is the process that the tests of the run-shared
// servers start. It does nothing in a normal run.
func TestRunSharedHelper(t *testing.T) { //nolint:paralleltest // a helper process runs only this test
	switch os.Getenv(sharedHelperEnv) {
	case "mysql":
		f := &mysqlFamily{name: mysqlServer.name, image: mysqlServer.image, tag: mysqlServer.tag, envPrefix: mysqlServer.envPrefix, fast: mysqlServer.fast}
		t.Cleanup(func() { require.NoError(t, closeShared(context.Background())) })
		port, _ := f.shared(t)
		fmt.Printf("SERVER %d\n", port)
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "crash":
		cli, err := newDockerClient()
		require.NoError(t, err)
		id, _, err := joinShared(t.Context(), cli, probeSpec)
		require.NoError(t, err)
		fmt.Printf("SERVER %s\n", id)
		os.Exit(0)
	default:
		t.Skip("only a process that a test of the run-shared servers starts runs this")
	}
}

type helper struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	server string
}

func startHelper(t *testing.T, mode string, run int) *helper {
	t.Helper()
	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), os.Args[0], "-test.run=^TestRunSharedHelper$", "-test.count=1") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), sharedHelperEnv+"="+mode, runKeyEnv+"="+strconv.Itoa(run))
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	h := &helper{cmd: cmd, stdin: stdin}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	scan := bufio.NewScanner(stdout)
	for scan.Scan() {
		if v, ok := strings.CutPrefix(scan.Text(), "SERVER "); ok {
			h.server = v
			go func() { _, _ = io.Copy(io.Discard, stdout) }()
			return h
		}
	}
	t.Fatalf("the helper %s ended before it joined a server", mode)
	return nil
}

func (h *helper) end(t *testing.T) {
	t.Helper()
	require.NoError(t, h.stdin.Close())
	require.NoError(t, h.cmd.Wait())
}

func serversOfRun(t *testing.T, cli *mobyclient.Client, run int) []string {
	t.Helper()
	list, err := cli.ContainerList(context.Background(), mobyclient.ContainerListOptions{All: true, Filters: mobyclient.Filters{}.Add("label", runSharedLabel+"="+strconv.Itoa(run))})
	require.NoError(t, err)
	ids := make([]string, 0, len(list.Items))
	for _, c := range list.Items {
		ids = append(ids, c.ID)
	}
	return ids
}

// TestTheTestProcessesOfARunShareOneMySQLServer starts two test processes
// of one run, as go test starts the test binaries of two packages. Both
// must reach one MySQL server, and the server must be gone once both end.
func TestTheTestProcessesOfARunShareOneMySQLServer(t *testing.T) {
	t.Parallel()
	sharedPool(t)
	cli, err := newDockerClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	run := os.Getpid()
	one := startHelper(t, "mysql", run)
	two := startHelper(t, "mysql", run)
	assert.Equal(t, one.server, two.server, "the processes of a run reach one MySQL server")
	assert.Len(t, serversOfRun(t, cli, run), 1)
	one.end(t)
	assert.Len(t, serversOfRun(t, cli, run), 1, "the server goes while a process of the run uses it")
	two.end(t)
	assert.Empty(t, serversOfRun(t, cli, run), "the server stays after the last process of the run")
}

// TestAServerOfARunWhoseGoCommandEndedIsRemoved leaves a server of a run
// whose go command is gone and one of a run that goes on, as test
// processes that crashed do. Only the server of the ended run may go.
func TestAServerOfARunWhoseGoCommandEndedIsRemoved(t *testing.T) {
	t.Parallel()
	sharedPool(t)
	cli, err := newDockerClient()
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	ended := exec.CommandContext(t.Context(), "true")
	require.NoError(t, ended.Run())
	goes := exec.CommandContext(context.WithoutCancel(t.Context()), "cat")
	keep, err := goes.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, goes.Start())
	t.Cleanup(func() {
		_ = keep.Close()
		_ = goes.Wait()
	})
	dead, live := ended.Process.Pid, goes.Process.Pid
	require.False(t, processAlive(dead))
	require.NoError(t, withRunLock(dead, func(dir string) error {
		return os.WriteFile(dir+"/"+probeSpec.kind+"."+strconv.Itoa(dead)+".1", nil, 0o644)
	}))
	stale, err := startShared(t.Context(), cli, probeSpec, runSharedName(probeSpec.kind, dead), dead)
	require.NoError(t, err)
	startHelper(t, "crash", live)
	t.Cleanup(func() {
		for _, id := range serversOfRun(t, cli, live) {
			_, _ = cli.ContainerRemove(context.WithoutCancel(t.Context()), id, mobyclient.ContainerRemoveOptions{Force: true})
		}
		_ = os.RemoveAll(runSharedDir(live))
	})
	require.Len(t, serversOfRun(t, cli, live), 1)

	require.NoError(t, reapStaleRuns(t.Context(), cli))
	assert.Empty(t, serversOfRun(t, cli, dead), "the server of an ended run stays")
	assert.NotContains(t, serversOfRun(t, cli, dead), stale)
	assert.Len(t, serversOfRun(t, cli, live), 1, "the server of a run that goes on was removed")
	assert.NoDirExists(t, runSharedDir(dead), "the registry of an ended run stays")
	assert.DirExists(t, runSharedDir(live), "the registry of a run that goes on was removed")
}
