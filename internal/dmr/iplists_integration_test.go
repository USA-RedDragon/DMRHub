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
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/mmdvm"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ipListWait is how long a test waits for an answer that a refused
// address never gets.
const ipListWait = 1500 * time.Millisecond

// loginAnswer sends an RPTL of the repeater 100001 to the MMDVM server at addr
// from a new socket and returns the command of the answer, or
// CommandUnknown for none within ipListWait.
func loginAnswer(t *testing.T, addr string) dmrd.Command {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "udp", addr)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	p, err := (&dmrd.Control{Command: dmrd.CommandRPTL, ID: 100001}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	_, err = conn.Write(p)
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(ipListWait)))
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return dmrd.CommandUnknown
	}
	return dmrd.Classify(buf[:n], dmrd.DialectMMDVM)
}

func mmdvmList(mode config.IPListMode, block, allow []string) config.IPLists {
	return config.IPLists{MMDVM: config.IPList{Mode: mode, Block: block, Allow: allow}}
}

// refuseLocal is an IP list that refuses the loopback addresses.
func refuseLocal() config.IPList {
	return config.IPList{Mode: config.IPListBlock, Block: []string{"127.0.0.0/8", "::1"}}
}

// TestIPListsRefuseMMDVMLogins checks that the IP list of the repeaters
// answers the login of a refused address with MSTNAK and takes the login
// of an accepted one, in block and allow mode, for an address and a CIDR
// range, and that the list of the peers leaves the hotspots alone.
func TestIPListsRefuseMMDVMLogins(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.StartServers(t)

		for _, c := range []struct {
			name  string
			lists config.IPLists
			want  dmrd.Command
		}{
			{"off", config.IPLists{}, dmrd.CommandRPTACK},
			{"block address", mmdvmList(config.IPListBlock, []string{"127.0.0.1"}, nil), dmrd.CommandMSTNAK},
			{"block range", mmdvmList(config.IPListBlock, []string{"127.0.0.0/8"}, nil), dmrd.CommandMSTNAK},
			{"block other range", mmdvmList(config.IPListBlock, []string{"10.0.0.0/8", "2001:db8::/32"}, nil), dmrd.CommandRPTACK},
			{"block mode ignores the allow list", mmdvmList(config.IPListBlock, nil, []string{"10.0.0.0/8"}), dmrd.CommandRPTACK},
			{"allow range", mmdvmList(config.IPListAllow, nil, []string{"127.0.0.0/30"}), dmrd.CommandRPTACK},
			{"allow other range", mmdvmList(config.IPListAllow, []string{"10.0.0.0/8"}, []string{"10.0.0.0/8"}), dmrd.CommandMSTNAK},
			{"allow nothing", mmdvmList(config.IPListAllow, nil, nil), dmrd.CommandMSTNAK},
			{"other lists only", config.IPLists{WebTransport: refuseLocal(), IPSC: refuseLocal(), OpenBridge: refuseLocal()}, dmrd.CommandRPTACK},
		} {
			stack.Hub.SetIPLists(c.lists)
			assert.Equal(t, c.want, loginAnswer(t, stack.MMDVMAddr), c.name)
		}
	})
}

// TestIPListsRefuseMMDVMOverIPv6 checks the IP list of the repeaters on an
// MMDVM server that listens on the IPv6 loopback address.
func TestIPListsRefuseMMDVMOverIPv6(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		cfg := *stack.Config
		cfg.DMR.MMDVM.Bind = "::1"
		cfg.DMR.MMDVM.Port = 0
		cfg.DMR.MMDVM.WebTransport.Enabled = false
		s, err := mmdvm.MakeServer(&cfg, stack.Hub, stack.DB, stack.PubSub, stack.KV, "test", "test")
		require.NoError(t, err)
		require.NoError(t, s.Start(context.Background()))
		t.Cleanup(func() { _ = s.Stop(context.Background()) })
		addr := s.Addr().String()

		for _, c := range []struct {
			name  string
			lists config.IPLists
			want  dmrd.Command
		}{
			{"off", config.IPLists{}, dmrd.CommandRPTACK},
			{"block address", mmdvmList(config.IPListBlock, []string{"::1"}, nil), dmrd.CommandMSTNAK},
			{"block range", mmdvmList(config.IPListBlock, []string{"::/64"}, nil), dmrd.CommandMSTNAK},
			{"block IPv4", mmdvmList(config.IPListBlock, []string{"127.0.0.1", "0.0.0.0/0"}, nil), dmrd.CommandRPTACK},
			{"allow address", mmdvmList(config.IPListAllow, nil, []string{"::1/128"}), dmrd.CommandRPTACK},
			{"allow other range", mmdvmList(config.IPListAllow, nil, []string{"2001:db8::/32", "127.0.0.1"}), dmrd.CommandMSTNAK},
		} {
			stack.Hub.SetIPLists(c.lists)
			assert.Equal(t, c.want, loginAnswer(t, addr), c.name)
		}
	})
}

// TestIPListsEndRefusedMMDVMSessions checks that a change of the IP list
// ends the session of a hotspot whose address the list now refuses, with
// MSTCL, and that its next login gets MSTNAK.
func TestIPListsEndRefusedMMDVMSessions(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.StartServers(t)
		c := testutils.NewMMDVMClient(100001, "", testPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		require.NoError(t, c.WaitReady(handshakeWait))

		stack.Hub.SetIPLists(mmdvmList(config.IPListBlock, []string{"127.0.0.0/8"}, nil))
		require.Eventually(t, func() bool { return c.MSTCLs() > 0 }, time.Second, 10*time.Millisecond, "the session of a refused address must end")
		assert.False(t, c.Ping(ipListWait), "a refused hotspot must get no pong")
		assert.Equal(t, dmrd.CommandMSTNAK, loginAnswer(t, stack.MMDVMAddr))
	})
}

// TestIPListsRefuseIPSCPeers checks that an IPSC repeater at an address
// that the IPSC list refuses cannot register, and registers once that list
// takes it, while the other lists refuse it.
func TestIPListsRefuseIPSCPeers(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.StartServers(t)

		stack.Hub.SetIPLists(config.IPLists{IPSC: config.IPList{Mode: config.IPListAllow, Allow: []string{"10.0.0.0/8"}}})
		c := testutils.NewIPSCClient(200001, ipscAuthKey)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.IPSCAddr))
		require.Error(t, c.WaitReady(ipListWait), "an IPSC repeater at a refused address must not register")

		stack.Hub.SetIPLists(config.IPLists{MMDVM: refuseLocal(), WebTransport: refuseLocal(), IPSC: config.IPList{Mode: config.IPListAllow, Allow: []string{"127.0.0.1"}}, OpenBridge: refuseLocal()})
		require.NoError(t, c.WaitReady(handshakeWait), "an IPSC repeater at an address that the IPSC list allows must register, whatever the other lists")
	})
}

// TestIPListsRefuseOpenBridgePeers checks that the IP list of the peers
// drops the packets from a refused peer address and sends nothing to a
// peer whose address it refuses, and that the other lists leave the peers
// alone.
func TestIPListsRefuseOpenBridgePeers(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.StartServers(t)
		obc := testutils.NewOpenBridgeClient(500001, obPassword)
		t.Cleanup(obc.Close)
		require.NoError(t, obc.Connect(stack.OpenBridgeAddr))
		local := obc.LocalAddr()
		stack.SeedPeer(t, 500001, 1000001, obPassword, local.IP.String(), local.Port, true, true)
		stack.SeedPeerRule(t, 500001, true, 1, 1)
		stack.SeedPeerRule(t, 500001, false, 1000001, 1000001)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		t.Cleanup(mc.Close)
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		stream := uint32(7100)
		check := func(name string, lists config.IPLists, want bool) {
			stack.Hub.SetIPLists(lists)
			stream++
			require.NoError(t, obc.SendGroupVoice(1000001, 1, stream))
			assert.Equal(t, want, len(mc.Drain(drainWait)) > 0, "%s: ingress", name)
			stream++
			require.NoError(t, mc.SendDMRD(makeGroupVoicePacket(1000001, 1, uint(stream), false)))
			time.Sleep(50 * time.Millisecond)
			require.NoError(t, mc.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, uint(stream), false)))
			assert.Equal(t, want, len(obc.Drain(drainWait)) > 0, "%s: egress", name)
		}
		check("off", config.IPLists{}, true)
		check("block", config.IPLists{OpenBridge: config.IPList{Mode: config.IPListBlock, Block: []string{"127.0.0.0/8"}}}, false)
		check("allow", config.IPLists{OpenBridge: config.IPList{Mode: config.IPListAllow, Allow: []string{"127.0.0.1"}}}, true)
		check("allow other", config.IPLists{OpenBridge: config.IPList{Mode: config.IPListAllow, Allow: []string{"192.0.2.0/24"}}}, false)
		check("other lists only", config.IPLists{WebTransport: refuseLocal(), IPSC: refuseLocal()}, true)
	})
}

// TestIPListsRefuseWebTransport checks that a browser radio at an address
// that the WebTransport list refuses gets no session while MMDVM over UDP
// takes the address, that one at an address that the list accepts logs in
// while the other lists refuse it, and
// that a change of the list ends the session of an address that it now
// refuses.
func TestIPListsRefuseWebTransport(t *testing.T) {
	t.Parallel()
	forAllBackendsWith(t, []testutils.Backend{testutils.WebTransportOn()}, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100004, 1000001, testPassword)
		stack.StartServers(t)

		stack.Hub.SetIPLists(config.IPLists{WebTransport: config.IPList{Mode: config.IPListBlock, Block: []string{"127.0.0.1"}}})
		assert.Equal(t, dmrd.CommandRPTACK, loginAnswer(t, stack.MMDVMAddr), "the WebTransport list must leave MMDVM over UDP alone")
		refused, err := testutils.NewWebTransportPeer(100004, testPassword)
		require.NoError(t, err)
		t.Cleanup(refused.Close)
		require.ErrorContains(t, refused.Connect(stack.WebTransportAddr), "403", "a browser radio at a refused address must get no session")

		stack.Hub.SetIPLists(config.IPLists{MMDVM: refuseLocal(), IPSC: refuseLocal(), OpenBridge: refuseLocal(), WebTransport: config.IPList{Mode: config.IPListAllow, Allow: []string{"127.0.0.1"}}})
		browser, err := testutils.NewWebTransportPeer(100004, testPassword)
		require.NoError(t, err)
		t.Cleanup(browser.Close)
		require.NoError(t, browser.Connect(stack.WebTransportAddr))
		require.NoError(t, browser.WaitReady(handshakeWait))

		stack.Hub.SetIPLists(config.IPLists{WebTransport: config.IPList{Mode: config.IPListAllow, Allow: []string{"::1"}}})
		require.Eventually(t, browser.SessionClosed, time.Second, 10*time.Millisecond, "the session of a refused address must end")
	})
}

// TestIPListsChangeOnOneReplicaAppliesAtOnceOnAnother saves the IP lists
// in Setup on one replica. The other replica must refuse the address as
// soon as the save returns, and end the session that it holds for it; a
// list that does not parse is refused with the field of the list.
func TestIPListsChangeOnOneReplicaAppliesAtOnceOnAnother(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range allBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			l.SeedUser(t, 1000001, "N0CALL")
			l.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{})
			addrB := b.Addr(t, manager.MMDVM)
			c := testutils.NewMMDVMClient(100001, "", testPassword)
			t.Cleanup(c.Close)
			require.NoError(t, c.Connect(addrB))
			require.NoError(t, c.WaitReady(handshakeWait))

			code, _, raw := putSetting(t, l.First(), map[string]any{
				"dmr.ip-lists.mmdvm": map[string]any{"mode": "block", "block": []string{"10.1.2.3/8"}},
			})
			require.Equal(t, http.StatusBadRequest, code, raw)
			assert.Contains(t, raw["errors"], "dmr", raw)

			code, res, raw := putSetting(t, l.First(), map[string]any{
				"dmr.ip-lists.mmdvm": map[string]any{"mode": "block", "block": []string{"192.0.2.0/24", "127.0.0.1"}},
			})
			require.Equal(t, http.StatusOK, code, raw)
			other, ok := replicaOf(res, idOf(t, b))
			require.True(t, ok)
			require.True(t, other.Answered, "the other replica must take the save before it returns")
			assert.Equal(t, dmrd.CommandMSTNAK, loginAnswer(t, addrB), "the other replica must refuse the address at once")
			require.Eventually(t, func() bool { return c.MSTCLs() > 0 }, time.Second, 10*time.Millisecond, "the other replica must end the session of the address")

			code, _, raw = putSetting(t, l.First(), map[string]any{
				"dmr.ip-lists.mmdvm": map[string]any{"mode": "off"},
			})
			require.Equal(t, http.StatusOK, code, raw)
			assert.Equal(t, dmrd.CommandRPTACK, loginAnswer(t, addrB), "the other replica must take the address again")
		})
	}
}

// TestIPListsDropRefusedIPSCPeersOnEveryReplica saves on one replica a
// list that refuses the IPSC repeaters of both replicas. Each replica must
// drop its repeater at once, so that once the list takes the address
// again the repeater registers anew instead of staying registered.
func TestIPListsDropRefusedIPSCPeersOnEveryReplica(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range allBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			l.SeedUser(t, 1000001, "N0CALL")
			l.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
			l.SeedIPSCRepeater(t, 200002, 1000001, ipscAuthKey)
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{})
			ids := []uint{200001, 200002}
			addrs := []string{l.First().Addr(t, manager.IPSC), b.Addr(t, manager.IPSC)}
			for i, id := range ids {
				c := testutils.NewIPSCClient(uint32(id), ipscAuthKey) //nolint:gosec // the test IDs fit in 32 bits
				t.Cleanup(c.Close)
				require.NoError(t, c.Connect(addrs[i]))
				require.NoError(t, c.WaitReady(handshakeWait))
			}
			connected := func(id uint) time.Time {
				r, err := models.FindRepeaterByID(l.DB, id)
				require.NoError(t, err)
				return r.Connected
			}
			require.Eventually(t, func() bool { return !connected(200001).IsZero() && !connected(200002).IsZero() }, handshakeWait, 10*time.Millisecond)
			before := map[uint]time.Time{200001: connected(200001), 200002: connected(200002)}

			code, _, raw := putSetting(t, l.First(), map[string]any{
				"dmr.ip-lists.ipsc": map[string]any{"mode": "allow", "allow": []string{"192.0.2.0/24"}},
			})
			require.Equal(t, http.StatusOK, code, raw)
			code, _, raw = putSetting(t, l.First(), map[string]any{
				"dmr.ip-lists.ipsc": map[string]any{"mode": "off"},
			})
			require.Equal(t, http.StatusOK, code, raw)
			for _, id := range ids {
				assert.Eventually(t, func() bool { return connected(id).After(before[id]) }, 2*time.Second, 10*time.Millisecond,
					"replica of IPSC repeater %d must drop it at once, so that it registers anew", id)
			}
		})
	}
}
