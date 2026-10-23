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
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type applyError struct {
	Server  string `json:"server"`
	Running bool   `json:"running"`
	Error   string `json:"error"`
}

func applyErrors(t *testing.T, body []byte) []applyError {
	t.Helper()
	var res struct {
		ApplyErrors []applyError `json:"applyErrors"`
	}
	require.NoError(t, json.Unmarshal(body, &res))
	return res.ApplyErrors
}

func useTempConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
}

func loopback(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// seedLiveTraffic seeds an MMDVM repeater and an IPSC repeater that carries
// TG1 on TS1, and logs both in.
func seedLiveTraffic(t *testing.T, l *testutils.LiveStack) (*testutils.MMDVMClient, *testutils.IPSCClient) {
	t.Helper()
	l.SeedUser(t, 1000001, "N0CALL")
	l.SeedTalkgroup(t, 1, "TG1")
	l.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	l.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
	l.AssignTS1StaticTG(t, 200001, 1)

	mc := testutils.NewMMDVMClient(100001, "", testPassword)
	require.NoError(t, mc.Connect(l.Addr(t, manager.MMDVM)))
	t.Cleanup(mc.Close)
	require.NoError(t, mc.WaitReady(handshakeWait))
	ic := testutils.NewIPSCClient(200001, ipscAuthKey)
	require.NoError(t, ic.Connect(l.Addr(t, manager.IPSC)))
	t.Cleanup(ic.Close)
	require.NoError(t, ic.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	return mc, ic
}

// TestLiveMMDVMPortChange changes the MMDVM port with the config API. The
// old port stops answering, the new one takes a login, and the IPSC peer on
// the IPSC server, whose settings stay the same, stays registered and gets
// the traffic of the new MMDVM session.
func TestLiveMMDVMPortChange(t *testing.T) { //nolint:paralleltest // t.Setenv for the saved config file
	useTempConfigDir(t)
	l := testutils.SetupLiveStack(t)
	mc, ic := seedLiveTraffic(t, l)
	oldAddr := l.Addr(t, manager.MMDVM)
	ipscBefore := l.Manager.Server(manager.IPSC)

	newPort := testutils.FreeUDPPort(t)
	w := l.SetDMR(t, manager.MMDVM, map[string]any{"port": newPort})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, loopback(newPort), l.Addr(t, manager.MMDVM))
	assert.Equal(t, float64(newPort), l.DMRSetting(t, manager.MMDVM, "port"))
	assert.Eventually(t, func() bool { return mc.MSTCLs() > 0 }, 2*time.Second, 10*time.Millisecond,
		"the repeater on the old port must get an MSTCL")

	stale := testutils.NewMMDVMClient(100001, "", testPassword)
	require.NoError(t, stale.Connect(oldAddr))
	assert.Error(t, stale.WaitReady(time.Second), "the old port must not answer a login")
	stale.Close()

	mc2 := testutils.NewMMDVMClient(100001, "", testPassword)
	require.NoError(t, mc2.Connect(loopback(newPort)))
	defer mc2.Close()
	require.NoError(t, mc2.WaitReady(handshakeWait), "the new port must take a login")
	time.Sleep(settleDuration)

	assert.Same(t, ipscBefore, l.Manager.Server(manager.IPSC), "the IPSC server must not restart")
	require.NoError(t, mc2.SendDMRD(makeGroupVoicePacket(1000001, 1, 7101, false)))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, mc2.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, 7101, false)))
	assert.Len(t, ic.Drain(drainWait), 2, "the IPSC peer must get the call from the new MMDVM session")
	assert.Zero(t, ic.Downs(), "the IPSC peer must stay registered")
}

// TestLiveMMDVMBindFailure moves the MMDVM server to a port that another
// socket holds. The API reports it, the server keeps its old port and
// session, and the saved config keeps the old port.
func TestLiveMMDVMBindFailure(t *testing.T) { //nolint:paralleltest // t.Setenv for the saved config file
	useTempConfigDir(t)
	l := testutils.SetupLiveStack(t)
	mc, _ := seedLiveTraffic(t, l)
	oldAddr := l.Addr(t, manager.MMDVM)
	oldPort := l.Config.DMR.MMDVM.Port

	taken, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer func() { _ = taken.Close() }()
	takenAddr, ok := taken.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	takenPort := takenAddr.Port

	w := l.SetDMR(t, manager.MMDVM, map[string]any{"port": takenPort})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	errs := applyErrors(t, w.Body.Bytes())
	require.Len(t, errs, 1)
	assert.Equal(t, "mmdvm", errs[0].Server)
	assert.True(t, errs[0].Running)
	assert.Contains(t, errs[0].Error, "address already in use")

	assert.Equal(t, oldAddr, l.Addr(t, manager.MMDVM))
	assert.Equal(t, float64(oldPort), l.DMRSetting(t, manager.MMDVM, "port"))
	assert.Zero(t, mc.MSTCLs(), "the repeater must stay logged in")
	assert.True(t, mc.Ping(2*time.Second), "the repeater must still get pongs")

	again := testutils.NewMMDVMClient(100001, "", testPassword)
	require.NoError(t, again.Connect(oldAddr))
	defer again.Close()
	assert.NoError(t, again.WaitReady(handshakeWait), "the old port must still take a login")
}

// TestLiveIPSCNetworkIDChange changes only the network ID of the IPSC
// server. Its socket keeps the same address, so the new server binds it
// after the old one let it go.
func TestLiveIPSCNetworkIDChange(t *testing.T) { //nolint:paralleltest // t.Setenv for the saved config file
	useTempConfigDir(t)
	l := testutils.SetupLiveStack(t)
	mc, ic := seedLiveTraffic(t, l)
	addr := l.Addr(t, manager.IPSC)

	w := l.SetDMR(t, manager.IPSC, map[string]any{"network-id": 888888})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, addr, l.Addr(t, manager.IPSC))
	assert.Eventually(t, func() bool { return ic.Downs() > 0 }, 2*time.Second, 10*time.Millisecond,
		"the IPSC peer must be asked to deregister")
	assert.Zero(t, mc.MSTCLs(), "the MMDVM repeater must stay logged in")

	ic2 := testutils.NewIPSCClient(200001, ipscAuthKey)
	require.NoError(t, ic2.Connect(addr))
	defer ic2.Close()
	require.NoError(t, ic2.WaitReady(handshakeWait))
}

// TestLiveOpenBridgeToggle turns OpenBridge off and on with the config API.
// Off frees its port and hides the peer routes; on brings both back. The
// MMDVM repeater stays logged in throughout.
func TestLiveOpenBridgeToggle(t *testing.T) { //nolint:paralleltest // t.Setenv for the saved config file
	useTempConfigDir(t)
	l := testutils.SetupLiveStack(t)
	mc, _ := seedLiveTraffic(t, l)
	obAddr := l.Addr(t, manager.OpenBridge)

	w := testutils.Do(t, l.Router, l.Admin, http.MethodGet, "/api/v1/peers", nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = l.SetDMR(t, manager.OpenBridge, map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Nil(t, l.Manager.Server(manager.OpenBridge))
	freed, err := net.ResolveUDPAddr("udp", obAddr)
	require.NoError(t, err)
	c, err := net.ListenUDP("udp", freed)
	require.NoError(t, err, "the OpenBridge port must be free")
	require.NoError(t, c.Close())
	w = testutils.Do(t, l.Router, l.Admin, http.MethodGet, "/api/v1/peers", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = l.SetDMR(t, manager.OpenBridge, map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, obAddr, l.Addr(t, manager.OpenBridge))
	ob, ok := l.Manager.Server(manager.OpenBridge).(*openbridge.Server)
	require.True(t, ok)
	assert.Eventually(t, ob.IsEgressLeader, 2*openbridge.EgressLeaseTTL, 10*time.Millisecond)
	w = testutils.Do(t, l.Router, l.Admin, http.MethodGet, "/api/v1/peers", nil)
	assert.Equal(t, http.StatusOK, w.Code)

	assert.Zero(t, mc.MSTCLs(), "the MMDVM repeater must stay logged in")
	assert.True(t, mc.Ping(2*time.Second))
}

// TestLiveOpenBridgeEnableFailure turns OpenBridge on at a port that
// another socket holds. The API reports it and the saved config keeps
// OpenBridge off.
func TestLiveOpenBridgeEnableFailure(t *testing.T) { //nolint:paralleltest // t.Setenv for the saved config file
	useTempConfigDir(t)
	l := testutils.SetupLiveStack(t)
	w := l.SetDMR(t, manager.OpenBridge, map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	taken, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer func() { _ = taken.Close() }()

	takenAddr, ok := taken.LocalAddr().(*net.UDPAddr)
	require.True(t, ok)
	w = l.SetDMR(t, manager.OpenBridge, map[string]any{"enabled": true, "port": takenAddr.Port})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	errs := applyErrors(t, w.Body.Bytes())
	require.Len(t, errs, 1)
	assert.Equal(t, "openbridge", errs[0].Server)
	assert.False(t, errs[0].Running)
	assert.Nil(t, l.Manager.Server(manager.OpenBridge))
	assert.Nil(t, l.DMRSetting(t, manager.OpenBridge, "enabled"), "the saved config must keep OpenBridge off")
}

// TestLiveOpenBridgeDisableHandsOverEgress runs two replicas that share the
// KV store. The first holds the OpenBridge egress lease; turning its
// OpenBridge off must let the second take the lease.
func TestLiveOpenBridgeDisableHandsOverEgress(t *testing.T) { //nolint:paralleltest // t.Setenv for the saved config file
	useTempConfigDir(t)
	l := testutils.SetupLiveStack(t)
	ob1, ok := l.Manager.Server(manager.OpenBridge).(*openbridge.Server)
	require.True(t, ok)
	require.True(t, ob1.IsEgressLeader())

	r2 := l.SpawnSecondReplica(t)
	assert.False(t, r2.OpenBridgeServer.IsEgressLeader())

	w := l.SetDMR(t, manager.OpenBridge, map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Eventually(t, r2.OpenBridgeServer.IsEgressLeader, 3*openbridge.EgressLeaseTTL, 10*time.Millisecond,
		"the other replica must take the egress lease")

	w = l.SetDMR(t, manager.OpenBridge, map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ob1, ok = l.Manager.Server(manager.OpenBridge).(*openbridge.Server)
	require.True(t, ok)
	time.Sleep(openbridge.EgressLeaseTTL)
	assert.False(t, ob1.IsEgressLeader(), "a restarted server must not take the lease from the holder")
	assert.True(t, r2.OpenBridgeServer.IsEgressLeader())
}
