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

package manager_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem/modemtest"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"github.com/stretchr/testify/require"
)

func TestApplyTurnsTheModemOnRestartsItAndTurnsItOff(t *testing.T) {
	t.Parallel()
	stack := testutils.SetupIntegrationStack(t)
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedMMDVMRepeater(t, 311860, 1000001, password)
	opened := make(chan *modemtest.Fake, 8)
	t.Cleanup(func() {
		for {
			select {
			case f := <-opened:
				f.Close()
			default:
				return
			}
		}
	})
	opts := modem.Options{Reply: 200 * time.Millisecond, Reopen: 50 * time.Millisecond, Retry: 50 * time.Millisecond,
		Open: func(string, int) (modem.Port, error) {
			host, dev := net.Pipe()
			opened <- modemtest.New(dev, 1)
			return host, nil
		}}
	cfg := stack.Config
	m := manager.New(manager.Deps{Hub: stack.Hub, DB: stack.DB, PubSub: stack.PubSub, KV: stack.KV, Version: "test", Commit: "test", ModemOptions: &opts})
	require.NoError(t, m.Start(context.Background(), cfg))
	t.Cleanup(func() { m.Stop(context.Background()) })
	require.Nil(t, m.Server(manager.Modem), "the modem is off by default")

	next := *cfg
	next.DMR.Modem.Enabled, next.DMR.Modem.Port, next.DMR.Modem.RepeaterID = true, "/dev/fake", 311860
	next.DMR.Modem.RXFrequency, next.DMR.Modem.TXFrequency = 438800000, 438800000
	require.Empty(t, m.Apply(&next))
	linked := func() *modemtest.Fake {
		select {
		case f := <-opened:
			require.True(t, f.WaitFor(wait, func(f *modemtest.Fake) bool { return f.Count(mmdvm.CommandGetStatus) >= 2 }))
			return f
		case <-time.After(wait):
			t.Fatal("the modem was not opened")
			return nil
		}
	}
	first := linked()
	require.Equal(t, "/dev/fake", m.Addr(manager.Modem).String())

	duplex := next
	duplex.DMR.Modem.Duplex = true
	require.Empty(t, m.Apply(&duplex))
	second := linked()
	require.True(t, second.WaitFor(0, func(f *modemtest.Fake) bool { return f.Duplex() }))
	require.True(t, first.WaitFor(wait, func(f *modemtest.Fake) bool {
		fr := f.Frames()
		return fr[len(fr)-1].Command == mmdvm.CommandSetMode && fr[len(fr)-1].Payload[0] == byte(mmdvm.ModeIdle)
	}), "the first modem was not left idle")

	off := duplex
	off.DMR.Modem.Enabled = false
	require.Empty(t, m.Apply(&off))
	require.Nil(t, m.Server(manager.Modem))
}
