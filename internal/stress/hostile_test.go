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
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/require"
)

func udpTo(t *testing.T, addr string) *net.UDPConn {
	t.Helper()
	raddr, err := net.ResolveUDPAddr("udp", addr)
	require.NoError(t, err)
	c, err := net.DialUDP("udp", nil, raddr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestHostileInput sends malformed and oversized packets to every port,
// floods the logins, spoofs repeaters, tries to fill the message store and
// opens websockets from other sites, while the call mix runs. The hub must
// not crash, must not route what it must not, and the legitimate traffic
// must still get through.
func TestHostileInput(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "hostile")
	r := newRig(t, s, res, logs, rigOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.runLanes(ctx, 10*time.Hour, loadMix(), s.Duty*0.6, r.top.n-1, nil)
	rnd := rand.New(rand.NewPCG(5, 5)) //nolint:gosec
	spare := r.top.n - 1

	step(t, "malformed", func(t *testing.T) {
		n := 0
		for _, addr := range []string{r.stack.MMDVMAddr, r.stack.OpenBridgeAddr, r.stack.IPSCAddr} {
			c := udpTo(t, addr)
			for _, p := range malformed(rnd) {
				_, _ = c.Write(p)
				n++
			}
		}
		for _, p := range malformed(rnd) {
			if (bytes.HasPrefix(p, []byte("DMRD")) && len(p) >= 53) || bytes.HasPrefix(p, []byte("RPT")) {
				continue
			}
			_ = r.clients[spare].SendRaw(p)
			n++
		}
		res.set("malformed_packets", float64(n))
		time.Sleep(time.Second)
		if !r.clients[spare].Ping(5 * time.Second) {
			res.fail(t, "a repeater that sent malformed packets lost its session")
		}
	})

	step(t, "spoofed-repeater", func(t *testing.T) {
		c := udpTo(t, r.stack.MMDVMAddr)
		g := r.top.ts1[0]
		sc := voiceCall("spoofed", true, tgID(g), userID(0), 10, nil)
		r.playOn(func(p models.Packet) error {
			p.Repeater = rptID(0)
			if _, err := c.Write(p.Encode()); err != nil {
				return fmt.Errorf("spoofed write: %w", err)
			}
			return nil
		}, 0, false, sc, nil)
		time.Sleep(2 * time.Second)
	})

	step(t, "login-flood", func(t *testing.T) {
		g0 := runtime.NumGoroutine()
		socks := make([]*net.UDPConn, 50)
		for i := range socks {
			socks[i] = udpTo(t, r.stack.MMDVMAddr)
		}
		sent := 0
		start := time.Now()
		for i := range 5000 {
			var p [8]byte
			copy(p[:], "RPTL")
			id := uint32(400000 + rnd.IntN(500000)) //nolint:gosec
			if i%50 == 0 {
				id = uint32(rptID(0)) //nolint:gosec
			}
			binary.BigEndian.PutUint32(p[4:], id)
			_, _ = socks[i%len(socks)].Write(p[:])
			sent++
		}
		res.set("login_flood_packets", float64(sent))
		res.set("login_flood_s", time.Since(start).Seconds())
		late := testutils.NewMMDVMClient(uint32(rptID(spare)), "", rptPassword) //nolint:gosec
		t0 := time.Now()
		r.clients[spare].Close()
		require.NoError(t, late.Connect(r.stack.MMDVMAddr))
		if err := late.WaitReady(60 * time.Second); err != nil {
			res.fail(t, "a real repeater could not log in within 60s of a login flood: %v", err)
		}
		took := time.Since(t0)
		res.set("login_after_flood_ms", float64(took.Milliseconds()))
		if took > handshake {
			res.fail(t, "a real repeater took %s to log in after a flood of %d logins", took, sent)
		}
		r.clients[spare] = late
		r.listen(rptID(spare), late.ReceivedPackets())
		if !r.clients[0].Ping(5 * time.Second) {
			res.fail(t, "the session of a repeater whose ID the flood used is gone")
		}
		time.Sleep(3 * time.Second)
		res.set("login_flood_goroutines", float64(runtime.NumGoroutine()-g0))
	})

	step(t, "fill-message-store", func(t *testing.T) {
		before := counterValue("message_store_dropped_total")
		var rows0 int64
		quiet(r.stack.DB).Table("messages").Count(&rows0)
		j := 0
		sent := 0
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			sc := smsScript("sms-flood", false, userID(j), userID(spare), r.privateWant(rptID(j)), sms.FormatIP, enums.DataTypeRate12)
			sc.noPace = true
			r.play(spare, false, sc)
			sent++
		}
		time.Sleep(5 * time.Second)
		var rows1 int64
		quiet(r.stack.DB).Table("messages").Count(&rows1)
		res.set("flood_sms_sent", float64(sent))
		res.set("flood_sms_stored", float64(rows1-rows0))
		res.set("flood_sms_dropped_counted", counterValue("message_store_dropped_total")-before)
		res.set("flood_tracker_dropped", float64(r.stack.CallTracker.Dropped()))
	})

	step(t, "web", func(t *testing.T) {
		w := r.web(t)
		jar := w.account(t, r, accountBase+900)
		w.account(t, r, accountBase+901)
		conn, resp := testutils.DialWSFrom(t, w.srv.URL, "/ws/calls", jar, "https://evil.example")
		if conn != nil {
			conn.Close()
			res.fail(t, "a websocket from another site's origin was accepted")
		} else {
			res.set("cross_site_ws_status", float64(resp.StatusCode))
			_ = resp.Body.Close()
		}
		const floods = 120
		codes := make(chan int, floods)
		client := &http.Client{Timeout: 90 * time.Second}
		login := func(user, pass string) int {
			body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, w.srv.URL+"/api/v1/auth/login", bytes.NewReader(body))
			if err != nil {
				return 0
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return 0
			}
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		t0 := time.Now()
		for i := range floods {
			go func() { codes <- login(fmt.Sprintf("web%d", accountBase+900), fmt.Sprintf("wrong%d", i)) }()
		}
		time.Sleep(200 * time.Millisecond)
		g0 := time.Now()
		good := login(fmt.Sprintf("web%d", accountBase+901), testutils.TestPassword)
		res.set("login_during_flood_status", float64(good))
		res.set("login_during_flood_s", time.Since(g0).Seconds())
		limited := 0
		for range floods {
			if <-codes == http.StatusTooManyRequests {
				limited++
			}
		}
		res.set("login_flood_http_429", float64(limited))
		res.set("login_flood_http_s", time.Since(t0).Seconds())
		if limited == 0 {
			res.fail(t, "%d concurrent failed logins from one address were never rate limited", floods)
		}
	})

	cancel()
	r.waitLanes()
	time.Sleep(6 * time.Second)
	r.led.finish(0, true)
	r.checkDelivery()
	r.checkNoStuckStreams(15 * time.Second)
}

// malformed returns hostile packets: random octets of any length, every
// truncation of a DMRD packet and of the login messages, and oversized
// packets with valid tags.
func malformed(rnd *rand.Rand) [][]byte {
	var out [][]byte
	for range 300 {
		p := make([]byte, rnd.IntN(1500))
		for i := range p {
			p[i] = byte(rnd.Uint32())
		}
		out = append(out, p)
	}
	pkt := packetOf(randomBurst(rnd, dmrconst.FrameDataSync, uint(enums.DataTypeCSBK)), 0, userID(0), tgID(0), 0xBAD, true, false)
	full := pkt.Encode()
	for n := range len(full) {
		out = append(out, full[:n])
	}
	for _, tag := range []string{"DMRD", "DMRA", "DMRG", "RPTL", "RPTK", "RPTC", "RPTO", "RPTPING", "RPTCL", "RPTG", "DMRE", "BCKA", "BCSQ", "BCST", "BCVE"} {
		for _, n := range []int{len(tag), len(tag) + 1, len(tag) + 4, 60, 302, 306, 1400} {
			p := make([]byte, max(n, len(tag)))
			copy(p, tag)
			for i := len(tag); i < len(p); i++ {
				p[i] = byte(rnd.Uint32())
			}
			out = append(out, p)
		}
	}
	return out
}
