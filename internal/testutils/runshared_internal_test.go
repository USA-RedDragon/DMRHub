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
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	mobyclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
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
