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

package dmr_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// movingPlugin records what a plugin's serve sees.
type movingPlugin struct {
	mu      sync.Mutex
	serves  int
	ended   []error
	other   []string
	got     map[uint32]int
	allSeen chan struct{}
	want    int
}

func (p *movingPlugin) serve(_ context.Context, conn *plugin.Conn) error {
	p.mu.Lock()
	p.serves++
	p.mu.Unlock()
	for {
		m, err := conn.Recv()
		if err != nil {
			p.mu.Lock()
			p.ended = append(p.ended, err)
			p.mu.Unlock()
			return fmt.Errorf("cannot receive: %w", err)
		}
		p.mu.Lock()
		if b := m.GetBurst(); b != nil {
			p.got[b.GetSequence()]++
			if len(p.got) == p.want {
				select {
				case <-p.allSeen:
				default:
					close(p.allSeen)
				}
			}
		} else {
			p.other = append(p.other, m.String())
		}
		p.mu.Unlock()
	}
}

// TestAPluginMovesToAnotherReplicaWithoutLosingABurst drains the replica
// that holds a plugin's stream in the middle of a call to the plugin. The
// plugin moves to the other replica and gets every burst once, and its
// serve sees no end of the connection.
func TestAPluginMovesToAnotherReplicaWithoutLosingABurst(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		const (
			caller   = uint(1000001)
			repeater = uint(100001)
			pluginID = uint(9991)
			streamID = uint(4242)
			voice    = 60
		)
		stack.SeedUser(t, caller, "N0CALL")
		stack.SeedMMDVMRepeater(t, repeater, caller, testPassword)
		stack.StartServers(t)

		first := stack.Plugins(t)
		replica := stack.SpawnSecondReplica(t)
		second := replica.StartPlugins(t, stack)
		lb := testutils.NewBalancer(t, first.Server.Addr().String())

		p := &movingPlugin{got: map[uint32]int{}, allSeen: make(chan struct{}), want: voice + 2}
		first.RunAt(t, lb.Addr(), "Mover", []models.PluginRange{{First: pluginID, Last: pluginID}}, p.serve)

		c := testutils.NewMMDVMClient(uint32(repeater), "", testPassword)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		defer c.Close()
		require.NoError(t, c.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		drained := make(chan int, 1)
		require.NoError(t, c.SendDMRD(makePrivateVoiceHeaderPacket(caller, pluginID, streamID, false)))
		for i := uint(1); i <= voice; i++ {
			time.Sleep(60 * time.Millisecond)
			if i == 10 {
				lb.SetBackend(second.Server.Addr().String())
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					drained <- first.Server.Drain(ctx)
				}()
			}
			pkt := makePrivateVoicePacket(pluginID, streamID)
			pkt.Seq = i
			pkt.DTypeOrVSeq = (i - 1) % 6
			require.NoError(t, c.SendDMRD(pkt))
		}
		time.Sleep(60 * time.Millisecond)
		term := makePrivateVoiceTermPacket(pluginID, streamID)
		term.Seq = voice + 1
		require.NoError(t, c.SendDMRD(term))

		select {
		case left := <-drained:
			assert.Equal(t, 0, left, "the plugin's stream on the first replica ends")
		case <-time.After(30 * time.Second):
			t.Fatal("the drain did not end")
		}
		select {
		case <-p.allSeen:
		case <-time.After(10 * time.Second):
		}
		time.Sleep(2 * time.Second)

		p.mu.Lock()
		defer p.mu.Unlock()
		for seq := uint32(0); seq <= voice+1; seq++ {
			assert.Equal(t, 1, p.got[seq], "burst %d reaches the plugin once", seq)
		}
		assert.Equal(t, 1, p.serves, "serve runs once")
		assert.Empty(t, p.ended, "serve sees no end of the connection")
		assert.Empty(t, p.other, "serve sees only the bursts")
	})
}
