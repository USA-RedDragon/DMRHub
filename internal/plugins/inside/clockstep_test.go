//go:build clockstep

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

package inside

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils/clockstep"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/require"
)

type sentStream struct {
	pluginv1.PluginService_ConnectServer
}

func (sentStream) Send(*pluginv1.ConnectResponse) error {
	return nil
}

func TestClockStepForwardDoesNotRetireAPluginInACall(t *testing.T) { //nolint:paralleltest
	m := &Manager{d: Deps{Timing: Timing{Quiet: time.Minute, MaxWait: 2 * time.Hour}}}
	r := &run{id: 1, ready: make(chan struct{}), ended: make(chan struct{})}
	close(r.ready)
	tr := &tracked{PluginService_ConnectServer: sentStream{}, r: r}
	require.NoError(t, tr.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Burst{Burst: &pluginv1.Burst{}}}))
	clockstep.Step(t, time.Hour)
	m.retire(r, time.Now())
	m.wg.Wait()
	require.False(t, r.stopping.Load(), "a step of the wall clock retired a plugin that sent a burst just now")
}
