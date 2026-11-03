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
	"os"
	"path/filepath"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRemover struct {
	remove  error
	inspect error
	state   container.ContainerState
}

func (f fakeRemover) ContainerRemove(context.Context, string, mobyclient.ContainerRemoveOptions) (mobyclient.ContainerRemoveResult, error) {
	return mobyclient.ContainerRemoveResult{}, f.remove
}

func (f fakeRemover) ContainerInspect(context.Context, string, mobyclient.ContainerInspectOptions) (mobyclient.ContainerInspectResult, error) {
	if f.inspect != nil {
		return mobyclient.ContainerInspectResult{}, f.inspect
	}
	return mobyclient.ContainerInspectResult{Container: container.InspectResponse{State: &container.State{Status: f.state}}}, nil
}

// TestAStaleServerThatAnotherRunRemovesCountsAsRemoved removes a stale
// server that another go test run is removing at the same time, or has
// removed. Docker then refuses the removal with "removal of container ...
// is already in progress", or answers that the container is gone; neither
// is an error of the run, whose tests would fail on it.
func TestAStaleServerThatAnotherRunRemovesCountsAsRemoved(t *testing.T) {
	t.Parallel()
	inProgress := cerrdefs.ErrInvalidArgument.WithMessage("removal of container abc is already in progress")
	for _, tc := range []struct {
		name string
		cli  fakeRemover
		ok   bool
	}{
		{name: "removed", cli: fakeRemover{}, ok: true},
		{name: "in progress, not yet marked", cli: fakeRemover{remove: inProgress, state: container.StateExited}, ok: true},
		{name: "in progress, marked", cli: fakeRemover{remove: inProgress, state: container.StateRemoving}, ok: true},
		{name: "already gone", cli: fakeRemover{remove: cerrdefs.ErrNotFound.WithMessage("no such container: abc"), inspect: cerrdefs.ErrNotFound}, ok: true},
		{name: "refused and still there", cli: fakeRemover{remove: errors.New("permission denied"), state: container.StateExited}, ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := removeStale(context.Background(), tc.cli, "abc")
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestARegistryThatAStaleScanRemovesIsMadeAgain locks the registry of a
// run while the stale scan of another process removes that registry, as
// it does for the registry of a run whose go command ended: after the
// registry is made and before its lock is opened, and after the lock is
// opened and before it is held. The lock must hold a registry that is
// there, and the work in it must succeed.
func TestARegistryThatAStaleScanRemovesIsMadeAgain(t *testing.T) {
	t.Parallel()
	for _, step := range []string{stepMade, stepOpened} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "run")
			removed := false
			err := lockRegistry(dir, func(at string) {
				if at == step && !removed {
					removed = true
					reapRegistry(dir)
				}
			}, func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "probe.1.1"), nil, 0o644)
			})
			require.NoError(t, err)
			assert.True(t, removed, "the scan did not run")
			assert.FileExists(t, filepath.Join(dir, "probe.1.1"))
			assert.FileExists(t, filepath.Join(dir, "lock"))
		})
	}
}

// TestAStaleScanLeavesARegistryThatIsHeld scans the registry of an ended
// run while a process holds its lock, as the test of a run that sets up a
// registry for an ended run does. The registry must stay.
func TestAStaleScanLeavesARegistryThatIsHeld(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "run")
	require.NoError(t, lockRegistry(dir, nil, func(string) error {
		reapRegistry(dir)
		return nil
	}))
	assert.DirExists(t, dir)
}

// TestAStaleScanOfAVanishedRegistryIsNoError scans the registry of an
// ended run that another process removed already.
func TestAStaleScanOfAVanishedRegistryIsNoError(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "gone")
	reapRegistry(dir)
	assert.NoDirExists(t, dir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	reapRegistry(dir)
	assert.NoDirExists(t, dir, "a registry without a lock stayed")
}
