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

//go:build stress

package stress_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/require"
)

// xport is the set of endpoints of every transport, besides the MMDVM
// repeaters of the rig.
type xport struct {
	tg      uint
	sp      *testutils.SessionPeer
	ipsc    *testutils.IPSCClient
	ipscSrc *testutils.IPSCClient
	obSrc   *testutils.OpenBridgeClient
}

func (r *rig) addTransports(t *testing.T) *xport {
	t.Helper()
	st := r.stack
	x := &xport{tg: tgID(0)}
	st.SeedUser(t, xportUser, "XPORT")
	st.SeedMMDVMRepeater(t, spRptID, xportUser, rptPassword)
	st.AssignTS1StaticTG(t, spRptID, x.tg)
	st.SeedIPSCRepeater(t, ipscRptID, xportUser, ipscKey)
	st.AssignTS1StaticTG(t, ipscRptID, x.tg)
	st.SeedIPSCRepeater(t, ipscSrcID, xportUser, ipscKey)
	st.SeedPeer(t, obSrcPeerID, xportUser, obPassword, "127.0.0.1", 0, true, false)
	st.SeedPeerRule(t, obSrcPeerID, true, 1, dmrconst.MaxDMRAddress)

	sp, err := testutils.NewSessionPeer(spRptID, rptPassword, "")
	require.NoError(t, err)
	t.Cleanup(sp.Close)
	require.NoError(t, sp.Connect(st.MMDVMAddr))
	require.NoError(t, sp.WaitReady(handshake))
	x.sp = sp
	r.extra = map[int][]uint{0: {spRptID, ipscRptID}}
	for _, p := range []**testutils.IPSCClient{&x.ipsc, &x.ipscSrc} {
		id := uint32(ipscRptID)
		if p == &x.ipscSrc {
			id = ipscSrcID
		}
		c := testutils.NewIPSCClient(id, ipscKey)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(st.IPSCAddr))
		require.NoError(t, c.WaitReady(handshake))
		*p = c
	}
	x.obSrc = testutils.NewOpenBridgeClient(obSrcPeerID, obPassword)
	t.Cleanup(x.obSrc.Close)
	require.NoError(t, x.obSrc.Connect(st.OpenBridgeAddr))

	r.listen(spRptID, sp.ReceivedPackets())
	r.recvWG.Add(1)
	go func() {
		defer r.recvWG.Done()
		ch := x.ipsc.ReceivedPackets()
		for {
			select {
			case <-r.stop:
				return
			case b := <-ch:
				r.led.receiveIPSC(ipscRptID, &b)
			}
		}
	}()
	time.Sleep(time.Second)
	return x
}

type xsource struct {
	name    string
	foreign bool
	ingress uint
	send    func(p models.Packet, last bool) error
}

func (x *xport) sources(r *rig) []xsource {
	return []xsource{
		{name: "mmdvm", ingress: rptID(0), send: func(p models.Packet, _ bool) error { return r.clients[0].SendDMRD(p) }},
		{name: "sessionpeer", ingress: spRptID, send: func(p models.Packet, _ bool) error { return x.sp.SendDMRD(p) }},
		{name: "openbridge-v1", ingress: obSrcPeerID, send: func(p models.Packet, _ bool) error {
			p.Repeater = obSrcPeerID
			return x.obSrc.SendRawPacket(p)
		}},
		{name: "openbridge-v5", ingress: obSrcPeerID, send: func(p models.Packet, _ bool) error {
			p.Repeater = obSrcPeerID
			return x.obSrc.SendEnhancedPacket(p, 5)
		}},
		{name: "ipsc", foreign: true, ingress: ipscSrcID, send: func(p models.Packet, last bool) error {
			h := layer2.Hint{Kind: layer2.KindData, DataType: enums.DataType(p.DTypeOrVSeq), HasDataType: true} //nolint:gosec
			if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
				pos, _ := enums.VoiceBurstAt(int(p.DTypeOrVSeq)) //nolint:gosec
				h = layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: pos}
			}
			return x.ipscSrc.SendBurst(p.Src, p.Dst, p.GroupCall, p.Slot, uint32(p.StreamID), layer2.RawBurst(p.DMRData), h, last) //nolint:gosec
		}},
	}
}

// targets of a group call on the talkgroup of the transports from a
// source: the rig repeaters that carry it, the session peer, the IPSC
// repeater and, for the sources that are not peers, the OpenBridge peer.
func (x *xport) want(r *rig, src xsource) []uint {
	var w []uint
	for i := range r.top.n {
		if (r.top.ts1[i] == 0 || r.top.ts2[i] == 0) && rptID(i) != src.ingress {
			w = append(w, rptID(i))
		}
	}
	if src.ingress != spRptID {
		w = append(w, spRptID)
	}
	w = append(w, ipscRptID)
	if r.ob != nil {
		w = append(w, obPeerID)
	}
	return w
}

// TestTransports sends voice, SMS and a CSBK from every transport to every
// other on one talkgroup while the call mix loads the MMDVM repeaters, and
// checks that each target gets each burst once and unchanged (on IPSC, the
// same voice and data payloads). It also checks that a peer that speaks
// HBRP can log in, and runs the external clients when DMRHUB_STRESS_EXT_DIR
// names them.
func TestTransports(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "transports")
	r := newRig(t, s, res, logs, rigOptions{withOB: true})
	x := r.addTransports(t)
	col := &collector{}
	r.led.onDone = col.add

	ctx, cancel := context.WithTimeout(context.Background(), s.Duration)
	defer cancel()
	r.runLanes(ctx, s.Duration, callMix(), s.Duty, 0, func(i int, slot2 bool) bool { return i == 0 && !slot2 })
	rnd := rand.New(rand.NewPCG(3, 3)) //nolint:gosec
	byTransport := map[string]int{}
	for round := 0; ctx.Err() == nil; round++ {
		for _, src := range x.sources(r) {
			for _, p := range []string{"voice", "sms", "csbk"} {
				if ctx.Err() != nil {
					break
				}
				sc := x.payload(r, rnd, p, src)
				sc.label = src.name + "/" + p
				r.playVia(src, sc)
				byTransport[sc.label]++
			}
		}
	}
	cancel()
	r.waitLanes()
	time.Sleep(8 * time.Second)
	r.waitDeliveries(3*time.Second, 45*time.Second)
	r.led.finish(0, true)
	r.checkDelivery()
	res.set("ipsc_unmatched_bursts", float64(r.led.loose.Load()))
	res.extra("transport_calls", byTransport)
	r.checkRows(col.all())
	r.checkNoStuckStreams(15 * time.Second)

	step(t, "hbrp", func(t *testing.T) { r.checkHBRP(t) })
	step(t, "external", func(t *testing.T) { r.checkExternal(t, s, res) })
}

func (x *xport) payload(r *rig, rnd *rand.Rand, kind string, src xsource) script {
	w := x.want(r, src)
	from := uint(3109200 + r.streams.Load()%500)
	switch kind {
	case "sms":
		s := smsScript("sms", true, x.tg, from, w, sms.FormatIP, enums.DataTypeRate12)
		return s
	case "csbk":
		var cs pdu.CSBK
		err := cs.SetPreamble(pdu.PreamblePDU{Group: true, TargetAddress: uint32(x.tg), SourceAddress: uint32(from)}) //nolint:gosec
		var b []burst
		if err == nil {
			raw, e := csbkRaw(&cs)
			err = e
			b = []burst{dataBurst(raw)}
		}
		return script{kind: "csbk", group: true, dst: x.tg, src: from, want: w, bursts: b, rowKind: "csbk", err: err}
	}
	return voiceCall("voice", true, x.tg, from, between(12, 30)(rnd), w)
}

// playVia plays s from a transport source. The hub gives an IPSC call a
// stream of its own, so the ledger finds its bursts by their addresses.
func (r *rig) playVia(src xsource, s script) {
	if s.err != nil {
		r.res.fail(r.t, "building %s: %v", s.label, s.err)
		return
	}
	if src.foreign {
		s.rowKind = ""
	}
	stream := uint(r.streams.Add(1))
	bursts, err := stampStream(s.bursts, stream)
	if err != nil {
		r.res.fail(r.t, "stamping %s: %v", s.label, err)
		return
	}
	s.bursts = bursts
	c := &callRec{stream: stream, kind: s.kind, label: s.label, src: s.src, dst: s.dst, group: s.group, ingress: src.ingress, rowKind: s.rowKind, foreign: src.foreign}
	if err := r.led.register(c, s.bursts, s.want); err != nil {
		r.res.fail(r.t, "%v", err)
		return
	}
	c.loose = map[uint]bool{ipscRptID: true}
	next := time.Now()
	for k, b := range s.bursts {
		time.Sleep(time.Until(next))
		next = next.Add(burstGap)
		r.led.willSend(c, k)
		if err := src.send(packetOf(b, k, s.src, s.dst, stream, s.group, false), k == len(s.bursts)-1); err != nil {
			r.res.inc("send_errors")
		}
	}
	c.done.Store(true)
	time.Sleep(300 * time.Millisecond)
}

// checkHBRP logs a peer in with the HomeBrew Repeater Protocol of the
// published specification (dmrd.DialectHBRP), which is not the MMDVM
// network protocol.
func (r *rig) checkHBRP(t *testing.T) {
	const id = 319500
	r.stack.SeedMMDVMRepeater(t, id, xportUser, rptPassword)
	var cfg dmrd.HBRPConfig
	copy(cfg[:], "RPTC")
	for i := 4; i < len(cfg); i++ {
		cfg[i] = ' '
	}
	cfg.SetID(id)
	_ = cfg.SetCallsign([]byte("H9BRP"))
	_ = cfg.SetRXFrequency(434787500)
	_ = cfg.SetTXFrequency(434787500)
	_ = cfg.SetColorCode(1)
	peer, err := dmrdsession.NewPeer(&dmrdsession.PeerConfig{
		Dialect: dmrd.DialectHBRP, ID: id, Password: []byte(rptPassword), Config: cfg[:],
		Retry: 200 * time.Millisecond, Timeout: 5 * time.Second,
	})
	if err != nil {
		r.res.fail(t, "hbrp: making the peer: %v", err)
		return
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	target, err := net.ResolveUDPAddr("udp", r.stack.MMDVMAddr)
	require.NoError(t, err)
	start := time.Now()
	var mu sync.Mutex
	up := make(chan struct{})
	var once sync.Once
	var replies int
	run := func(out []dmrdsession.Output) {
		for _, o := range out {
			switch o.Kind { //nolint:exhaustive
			case dmrdsession.OutputSend:
				_, _ = conn.WriteToUDP(o.Packet, target)
			case dmrdsession.OutputUp:
				once.Do(func() { close(up) })
			}
		}
	}
	go func() {
		buf := make([]byte, dmrdsession.MaxPacket)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			mu.Lock()
			replies++
			var out [dmrdsession.MaxOutputs]dmrdsession.Output
			run(peer.Packet(time.Since(start), buf[:n], out[:0]))
			mu.Unlock()
		}
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-up:
			r.res.set("hbrp_login", 1)
			return
		case <-tick.C:
			mu.Lock()
			var out [dmrdsession.MaxOutputs]dmrdsession.Output
			run(peer.Tick(time.Since(start), out[:0]))
			mu.Unlock()
		case <-deadline:
			mu.Lock()
			n := replies
			mu.Unlock()
			r.res.set("hbrp_login", 0)
			r.res.set("hbrp_replies", float64(n))
			r.res.fail(t, "hbrp: a peer that speaks HBRP (dmrd.DialectHBRP) did not log in within 5s; the hub sent %d packets back", n)
			return
		}
	}
}

// extClient is an external program of the external client lane: how the
// battery runs it and what its log says when it hears a call.
type extClient struct {
	name   string
	slot2  bool
	peer   bool
	log    string
	marker string
}

func extClients() []extClient {
	return []extClient{
		{name: "dmrgateway", log: "mmdvmhost.log", marker: "received network voice header"},
		{name: "hblink3", peer: true, marker: "GROUP CALL START"},
		{name: "hblink4", peer: true, marker: "RX stream started"},
		{name: "mmdvm_bridge", slot2: true, marker: "received network voice header"},
		{name: "ysf2dmr"},
	}
}

// checkExternal runs the external clients that DMRHUB_STRESS_EXT_DIR
// holds (see its PINS.md) against the hub of the rig: each logs in as a
// repeater, and HBlink also as an OpenBridge peer, and must hear the calls
// that a rig repeater makes on its talkgroup. They run only inside a
// network namespace with loopback alone (hack/stress.sh runs the area
// under unshare -r -n), so nothing reaches a public network.
// DMRHUB_STRESS_EXT names the clients to run.
func (r *rig) checkExternal(t *testing.T, s settings, res *result) {
	if s.Ext == "" {
		t.Skip("DMRHUB_STRESS_EXT_DIR is not set; the external clients are not run")
	}
	if _, err := os.Stat(filepath.Join(s.Ext, "PINS.md")); errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s has no PINS.md", s.Ext)
	}
	if os.Getenv("DMRHUB_STRESS_NETNS") == "" {
		t.Skip("the external clients run only in a network namespace; run the area through hack/stress.sh")
	}
	want := map[string]bool{}
	for _, n := range strings.Fields(os.Getenv("DMRHUB_STRESS_EXT")) {
		want[n] = true
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "hack", "stress-ext.sh"))
	require.NoError(t, err)
	hubPort := portOf(r.stack.MMDVMAddr)
	obPort := portOf(r.stack.OpenBridgeAddr)
	for k, ec := range extClients() {
		if len(want) > 0 && !want[ec.name] {
			continue
		}
		step(t, ec.name, func(t *testing.T) {
			r.runExternal(t, s, res, ec, k, script, hubPort, obPort)
		})
	}
}

func portOf(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

func (r *rig) runExternal(t *testing.T, s settings, res *result, ec extClient, k int, script string, hubPort, obPort int) {
	const pass = "passw0rd"
	id := uint(319800 + k)    //nolint:gosec
	obID := uint(3109300 + k) //nolint:gosec
	tg := tgID(0)
	r.stack.SeedMMDVMRepeater(t, id, xportUser, pass)
	if ec.slot2 {
		r.stack.AssignTS2StaticTG(t, id, tg)
	} else {
		r.stack.AssignTS1StaticTG(t, id, tg)
	}
	if ec.peer {
		obLocal := hubPort + 21
		if ec.name == "hblink4" {
			obLocal = hubPort + 32
		}
		r.stack.SeedPeer(t, obID, xportUser, pass, "127.0.0.1", obLocal, true, true)
		r.stack.SeedPeerRule(t, obID, true, 1, dmrconst.MaxDMRAddress)
		r.stack.SeedPeerRule(t, obID, false, 1, dmrconst.MaxDMRAddress)
	}
	dir, err := filepath.Abs(filepath.Join(s.Results, "ext-"+ec.name))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	out, err := os.Create(filepath.Join(dir, ec.name+".out"))
	require.NoError(t, err)
	defer func() { _ = out.Close() }()
	cmd := exec.Command("bash", script, ec.name, dir, strconv.Itoa(hubPort), strconv.Itoa(obPort), //nolint:gosec,noctx
		strconv.Itoa(int(id)), strconv.Itoa(int(tg)), strconv.Itoa(int(obID)), strconv.Itoa(int(tg)+1)) //nolint:gosec
	cmd.Stdout, cmd.Stderr = out, out
	cmd.Env = append(os.Environ(), "DMRHUB_STRESS_EXT_DIR="+s.Ext)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		time.Sleep(time.Second)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()

	start := time.Now()
	logged := false
	for time.Since(start) < 40*time.Second {
		var n int64
		quiet(r.stack.DB).Table("repeaters").Where("id = ? AND connected > ?", id, start).Count(&n)
		if n > 0 {
			logged = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	res.set("ext_"+ec.name+"_login", map[bool]float64{true: 1, false: 0}[logged])
	if !logged {
		res.fail(t, "external %s did not log in to the hub within 40s; its log is in %s", ec.name, dir)
		return
	}
	res.set("ext_"+ec.name+"_login_s", time.Since(start).Seconds())
	if ec.marker == "" {
		return
	}
	time.Sleep(3 * time.Second)
	src := -1
	for i := range r.top.n {
		if (!ec.slot2 && r.top.ts1[i] == 0) || (ec.slot2 && r.top.ts2[i] == 0) {
			src = i
			break
		}
	}
	require.GreaterOrEqual(t, src, 0, "no rig repeater carries the talkgroup of the external clients")
	const calls = 3
	for range calls {
		r.play(src, ec.slot2, voiceCall("ext-"+ec.name, true, tg, userID(src), 20, nil))
		time.Sleep(2 * time.Second)
	}
	time.Sleep(3 * time.Second)
	logPath := filepath.Join(dir, ec.name+".out")
	if ec.log != "" {
		logPath = filepath.Join(dir, ec.log)
	}
	b, _ := os.ReadFile(logPath) //nolint:gosec
	seen := strings.Count(string(b), ec.marker)
	res.set("ext_"+ec.name+"_calls_sent", calls)
	res.set("ext_"+ec.name+"_calls_heard", float64(seen))
	if seen < calls {
		res.fail(t, "external %s heard %d of %d calls (%q in %s)", ec.name, seen, calls, ec.marker, logPath)
	}
}
