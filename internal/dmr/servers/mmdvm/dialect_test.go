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

package mmdvm

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	testSalt     = 0x0A0B0C0D
	testPassword = "s3cret"
)

func allTags() []dmrd.Command {
	return []dmrd.Command{
		dmrd.CommandDMRD, dmrd.CommandDMRA, dmrd.CommandDMRG, dmrd.CommandRPTL, dmrd.CommandRPTACK,
		dmrd.CommandMSTACK, dmrd.CommandRPTK, dmrd.CommandRPTC, dmrd.CommandRPTO, dmrd.CommandRPTPING,
		dmrd.CommandMSTPONG, dmrd.CommandMSTPING, dmrd.CommandRPTPONG, dmrd.CommandRPTCL, dmrd.CommandMSTCL,
		dmrd.CommandMSTNAK, dmrd.CommandRPTSBKN, dmrd.CommandRPTG, dmrd.CommandRPTRSSI, dmrd.CommandRPTINTR,
	}
}

// TestNoPacketIsValidInBothDialects tries every tag at every length up to
// the largest packet, filled with '0', which is a valid octet of a binary
// ID, a hex digit and a decimal digit. Only DMRD may be valid in both the
// MMDVM network protocol and HBRP.
func TestNoPacketIsValidInBothDialects(t *testing.T) {
	t.Parallel()
	for _, c := range allTags() {
		for n := len(c.String()); n <= dmrdsession.MaxPacket; n++ {
			p := append([]byte(c.String()), bytes.Repeat([]byte{'0'}, n-len(c.String()))...)
			_, mmdvm := dmrd.RepeaterID(p, dmrd.DialectMMDVM)
			_, hbrp := dmrd.RepeaterID(p, dmrd.DialectHBRP)
			if mmdvm && hbrp && c != dmrd.CommandDMRD {
				t.Errorf("%s of %d octets is valid in both dialects", c, n)
			}
			d, ok := packetDialect(p)
			switch {
			case mmdvm && !hbrp:
				assert.True(t, ok && d == dmrd.DialectMMDVM, "%s of %d octets", c, n)
			case hbrp && !mmdvm:
				assert.True(t, ok && d == dmrd.DialectHBRP, "%s of %d octets", c, n)
			default:
				assert.False(t, ok, "%s of %d octets", c, n)
			}
		}
	}
}

func TestPacketDialectOfEachMessage(t *testing.T) {
	t.Parallel()
	for _, d := range []dmrd.Dialect{dmrd.DialectMMDVM, dmrd.DialectHBRP} {
		for _, c := range allTags() {
			p, err := (&dmrd.Control{Command: c, ID: 311860}).AppendBinary(nil, d)
			if err != nil {
				continue
			}
			got, ok := packetDialect(p)
			if c == dmrd.CommandRPTACK {
				assert.False(t, ok, "%s %s carries no repeater ID", d, c)
				continue
			}
			assert.True(t, ok, "%s %s", d, c)
			assert.Equal(t, d, got, "%s %s", d, c)
		}
		got, ok := packetDialect(rptc(t, d, 311860))
		assert.True(t, ok)
		assert.Equal(t, d, got, "%s RPTC", d)
	}
	_, ok := packetDialect((&models.Packet{Signature: "DMRD", Repeater: 311860}).Encode())
	assert.False(t, ok, "DMRD is the same in both dialects")
}

func rptc(t *testing.T, d dmrd.Dialect, id uint32) []byte {
	t.Helper()
	if d == dmrd.DialectHBRP {
		var c dmrd.HBRPConfig
		c.Reset()
		c.SetID(id)
		require.NoError(t, errors.Join(
			c.SetCallsign([]byte("N0CALL")),
			c.SetRXFrequency(438000000),
			c.SetTXFrequency(430400000),
			c.SetPower(25),
			c.SetColorCode(1),
			c.SetLatitude([]byte("+52.5200")),
			c.SetLongitude([]byte("+013.4050")),
			c.SetHeight(30),
			c.SetLocation([]byte("Berlin")),
			c.SetDescription([]byte("HBRP repeater 20 ch")),
			c.SetURL([]byte("https://example.org")),
			c.SetSoftwareID([]byte("20261023")),
			c.SetPackageID([]byte("HBRP")),
		))
		return c[:]
	}
	var c dmrd.MMDVMConfig
	c.Reset()
	c.SetID(id)
	require.NoError(t, errors.Join(
		c.SetCallsign([]byte("N0CALL")),
		c.SetRXFrequency(440000000),
		c.SetTXFrequency(445000000),
		c.SetPower(10),
		c.SetColorCode(1),
		c.SetLatitude([]byte("+00.0000")),
		c.SetLongitude([]byte("+000.0000")),
		c.SetHeight(10),
		c.SetSlots(3),
	))
	return c[:]
}

// dialectRig is a started server with a real hub and call tracker, as
// MakeServer makes it, and a repeater at a UDP peer. The rig hands each
// packet of the repeater to the server as its listener does, and the
// server answers the peer.
type dialectRig struct {
	t     *testing.T
	s     *Server
	cfg   *config.Config
	db    *gorm.DB
	store kv.KV
	ps    pubsub.PubSub
	clock clock.Clock
	kv    *servers.KVClient
	peer  *net.UDPConn
	from  net.UDPAddr
}

// rigOption changes the stores or the clock of a rig before it starts its
// server.
type rigOption func(*dialectRig)

// withKV puts wrap around the KV store of the server and its hub.
func withKV(wrap func(kv.KV) kv.KV) rigOption {
	return func(r *dialectRig) { r.store = wrap(r.store) }
}

// withClock runs the hub and the server on c.
func withClock(c clock.Clock) rigOption {
	return func(r *dialectRig) { r.clock = c }
}

func newDialectRig(t *testing.T, ids ...uint) *dialectRig {
	t.Helper()
	return newRig(t, ids)
}

func newRig(t *testing.T, ids []uint, opts ...rigOption) *dialectRig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	cfg.DMR.MMDVM.Bind = "127.0.0.1"
	cfg.DMR.MMDVM.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = kvStore.Close()
	})
	for _, id := range ids {
		require.NoError(t, database.Create(&models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: id},
			Type:                  models.RepeaterTypeMMDVM,
			Password:              testPassword,
		}).Error)
	}
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	from := *peer.LocalAddr().(*net.UDPAddr) //nolint:forcetypeassert
	r := &dialectRig{
		t:     t,
		cfg:   &cfg,
		db:    database,
		store: kvStore,
		ps:    ps,
		clock: clock.Real(),
		peer:  peer,
		from:  from,
	}
	for _, o := range opts {
		o(r)
	}
	r.kv = servers.MakeKVClient(r.store)
	r.s = r.replica(r.store)
	return r
}

// replica starts a server with its own hub and call tracker on the
// database and the pubsub of the rig and on store, as a replica of a
// rolling restart.
func (r *dialectRig) replica(store kv.KV) *Server {
	r.t.Helper()
	calls := calltracker.NewCallTracker(r.db, r.ps, store)
	r.t.Cleanup(calls.Stop)
	h := hub.NewHub(r.db, store, r.ps, calls, hub.WithClock(r.clock))
	s, err := MakeServer(r.cfg, h, r.db, r.ps, store, "test", "test")
	require.NoError(r.t, err)
	s.newSalt = func() (uint32, error) { return testSalt, nil }
	require.NoError(r.t, s.Start(context.Background()))
	r.t.Cleanup(func() {
		_ = s.Stop(context.Background())
		h.Stop(context.Background())
	})
	return &s
}

func (r *dialectRig) send(p []byte) {
	r.sendTo(r.s, p)
}

// sendTo hands p from the peer to s, as the listener of s does.
func (r *dialectRig) sendTo(s *Server, p []byte) {
	s.handleFrom(context.Background(), udpEndpoint(r.from), p, s.clock.Now())
}

func (r *dialectRig) control(d dmrd.Dialect, c dmrd.Control) {
	p, err := c.AppendBinary(nil, d)
	require.NoError(r.t, err)
	r.send(p)
}

// next returns the next packet to the peer, and fails the test when none
// comes within answerWait.
func (r *dialectRig) next(what string) []byte {
	r.t.Helper()
	buf := make([]byte, dmrdsession.MaxPacket)
	require.NoError(r.t, r.peer.SetReadDeadline(time.Now().Add(answerWait)))
	n, _, err := r.peer.ReadFromUDP(buf)
	require.NoError(r.t, err, "no %s within %v", what, answerWait)
	return buf[:n]
}

func (r *dialectRig) expect(d dmrd.Dialect, want dmrd.Command) dmrd.Control {
	r.t.Helper()
	p := r.next(want.String())
	k, err := dmrd.DecodeControl(p, d)
	require.NoError(r.t, err, "%q", p)
	require.Equal(r.t, want, k.Command, "%q", p)
	return k
}

// handled waits until s has done the lookups, the repeater jobs and the
// connection log writes that the packets that the rig gave it started,
// for a test that checks the state that they leave or that nothing
// happened.
func (r *dialectRig) handled(s *Server) {
	s.lookups.Wait()
	s.records.Wait()
	s.hub.WaitRepeaterEvents()
}

// expectNothing fails the test when the server sent the peer anything
// for the packets that the rig gave it.
func (r *dialectRig) expectNothing() {
	r.t.Helper()
	r.handled(r.s)
	r.quiet(r.s)
}

// quiet fails the test when s sent the peer anything that the rig has not
// read. A marker that s sends after the call comes after each such packet.
func (r *dialectRig) quiet(s *Server) {
	r.t.Helper()
	marker := []byte("marker")
	_, err := s.Server.WriteToUDP(marker, &r.from)
	require.NoError(r.t, err)
	for {
		p := r.next("marker")
		if bytes.Equal(p, marker) {
			return
		}
		require.Failf(r.t, "unexpected packet", "%q", p)
	}
}

// link returns the session of s at the peer, or nil, with its state and
// dialect, read under the lock of s.
func (r *dialectRig) link(s *Server) (*link, dmrdsession.State, dmrd.Dialect) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.links[r.from.String()]
	if l == nil {
		return nil, dmrdsession.StateIdle, 0
	}
	return l, l.m.State(), l.dialect
}

// serves reports whether s holds a session of the repeater id.
func serves(s *Server, id uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.byID[id]
	return ok
}

func (r *dialectRig) login(d dmrd.Dialect, id uint32) {
	r.t.Helper()
	r.loginWith(d, id, rptc(r.t, d, id))
}

func (r *dialectRig) loginWith(d dmrd.Dialect, id uint32, config []byte) {
	r.t.Helper()
	ack := dmrd.CommandRPTACK
	if d == dmrd.DialectHBRP {
		ack = dmrd.CommandMSTACK
	}
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTL, ID: id})
	k := r.expect(d, ack)
	salt, ok := k.LoginSalt()
	require.True(r.t, ok)
	require.Equal(r.t, uint32(testSalt), salt)
	token, err := dmrdauth.Token(d, salt, []byte(testPassword))
	require.NoError(r.t, err)
	r.control(d, dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token})
	r.expect(d, ack)
	r.send(config)
	r.expect(d, ack)
	_, st, _ := r.link(r.s)
	require.Equal(r.t, dmrdsession.StateRunning, st)
	r.handled(r.s)
}

func TestLoginWithABadColorCodeIsAccepted(t *testing.T) {
	t.Parallel()
	for d, at := range map[dmrd.Dialect]int{dmrd.DialectMMDVM: 36, dmrd.DialectHBRP: 40} {
		for i, cc := range []string{"x1", "16", "99"} {
			t.Run(d.String()+"/"+cc, func(t *testing.T) {
				t.Parallel()
				id := uint32(311870 + 10*at + i)
				r := newDialectRig(t, uint(id))
				require.NoError(t, r.s.DB.Model(&models.Repeater{}).Where("id = ?", id).Update("color_code", 7).Error)
				config := rptc(t, d, id)
				require.Equal(t, "01", string(config[at:at+2]))
				copy(config[at:], cc)
				r.loginWith(d, id, config)

				rec, err := r.kv.GetRepeater(context.Background(), uint(id))
				require.NoError(t, err)
				assert.Zero(t, rec.ColorCode)
				assert.Equal(t, "N0CALL", rec.Callsign)
				var row models.Repeater
				require.NoError(t, r.s.DB.First(&row, id).Error)
				assert.Zero(t, row.ColorCode)
			})
		}
	}
}

func TestHBRPLoginIsAnsweredInHBRP(t *testing.T) {
	t.Parallel()
	r := newDialectRig(t, 311860)
	r.login(dmrd.DialectHBRP, 311860)

	rec, err := r.kv.GetRepeater(context.Background(), 311860)
	require.NoError(t, err)
	assert.Equal(t, "HBRP", rec.Dialect)
	assert.Equal(t, "Berlin", rec.Location)
	assert.Equal(t, "HBRP repeater 20 ch", rec.Description)
	assert.Equal(t, "https://example.org", rec.URL)
	assert.Equal(t, uint(438000000), rec.RXFrequency)
	assert.Zero(t, rec.Slots)
	var row models.Repeater
	require.NoError(t, r.s.DB.First(&row, 311860).Error)
	assert.Equal(t, "HBRP", row.Dialect)

	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandMSTPING, ID: 311860})
	assert.Equal(t, uint32(311860), r.expect(dmrd.DialectHBRP, dmrd.CommandRPTPONG).ID)

	_, _, d := r.link(r.s)
	r.s.sendBeacon(311860, udpEndpoint(r.from), d)
	r.expect(dmrd.DialectHBRP, dmrd.CommandRPTSBKN)

	r.s.closeSessions(context.Background())
	r.expect(dmrd.DialectHBRP, dmrd.CommandMSTCL)
}

func TestMMDVMLoginRecordsItsDialect(t *testing.T) {
	t.Parallel()
	r := newDialectRig(t, 311861)
	r.login(dmrd.DialectMMDVM, 311861)
	rec, err := r.kv.GetRepeater(context.Background(), 311861)
	require.NoError(t, err)
	assert.Equal(t, "MMDVM", rec.Dialect)
	assert.Equal(t, uint(3), rec.Slots)
	_, _, d := r.link(r.s)
	r.s.sendBeacon(311861, udpEndpoint(r.from), d)
	r.expect(dmrd.DialectMMDVM, dmrd.CommandRPTSBKN)
	r.s.closeSessions(context.Background())
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTCL)
}

func TestHBRPRefusalsAreInHBRP(t *testing.T) {
	t.Parallel()
	r := newDialectRig(t, 311862)
	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandMSTPING, ID: 311862})
	assert.Equal(t, uint32(311862), r.expect(dmrd.DialectHBRP, dmrd.CommandMSTNAK).ID)

	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandRPTL, ID: 999999})
	r.expect(dmrd.DialectHBRP, dmrd.CommandMSTNAK)

	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandRPTL, ID: 311862})
	r.expect(dmrd.DialectHBRP, dmrd.CommandMSTACK)
	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandRPTK, ID: 311862})
	r.expect(dmrd.DialectHBRP, dmrd.CommandMSTNAK)
	l, _, _ := r.link(r.s)
	require.Nil(t, l)
}

func TestMixedDialectLoginIsIgnored(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		login, then dmrd.Dialect
	}{
		{"MMDVM RPTL then HBRP RPTK", dmrd.DialectMMDVM, dmrd.DialectHBRP},
		{"HBRP RPTL then MMDVM RPTK", dmrd.DialectHBRP, dmrd.DialectMMDVM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newDialectRig(t, 311863)
			ack := dmrd.CommandRPTACK
			if tc.login == dmrd.DialectHBRP {
				ack = dmrd.CommandMSTACK
			}
			r.control(tc.login, dmrd.Control{Command: dmrd.CommandRPTL, ID: 311863})
			r.expect(tc.login, ack)
			token, err := dmrdauth.Token(tc.then, testSalt, []byte(testPassword))
			require.NoError(t, err)
			r.control(tc.then, dmrd.Control{Command: dmrd.CommandRPTK, ID: 311863, Token: token})
			r.expectNothing()
			r.send(rptc(t, tc.then, 311863))
			r.expectNothing()
			l, st, d := r.link(r.s)
			require.NotNil(t, l)
			assert.Equal(t, tc.login, d)
			assert.Equal(t, dmrdsession.StateAuth, st)
			_, ok := r.s.connected.Load(311863)
			assert.False(t, ok)
		})
	}
}

// TestLoginInTheOtherDialectEndsTheSession logs a repeater in, then has the
// same address log in again in the other dialect, as a repeater that
// restarts with new settings does.
func TestLoginInTheOtherDialectEndsTheSession(t *testing.T) {
	t.Parallel()
	r := newDialectRig(t, 311864)
	r.login(dmrd.DialectMMDVM, 311864)
	_, ok := r.s.connected.Load(311864)
	require.True(t, ok)

	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandRPTL, ID: 311864})
	r.expect(dmrd.DialectHBRP, dmrd.CommandMSTACK)
	_, ok = r.s.connected.Load(311864)
	assert.False(t, ok, "the old session should have ended")
	assert.False(t, serves(r.s, 311864))

	token, err := dmrdauth.Token(dmrd.DialectHBRP, testSalt, []byte(testPassword))
	require.NoError(t, err)
	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandRPTK, ID: 311864, Token: token})
	r.expect(dmrd.DialectHBRP, dmrd.CommandMSTACK)
	r.send(rptc(t, dmrd.DialectHBRP, 311864))
	r.expect(dmrd.DialectHBRP, dmrd.CommandMSTACK)
	rec, err := r.kv.GetRepeater(context.Background(), 311864)
	require.NoError(t, err)
	assert.Equal(t, "HBRP", rec.Dialect)
}

// TestResumeKeepsTheDialect hands a logged-in repeater to a new server, as
// a handoff to another replica does. The new server must take the session
// over in the dialect of the KV record, for a ping and for egress.
func TestResumeKeepsTheDialect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		stored  string
		dialect dmrd.Dialect
		ping    dmrd.Command
		pong    dmrd.Command
	}{
		{"HBRP", "HBRP", dmrd.DialectHBRP, dmrd.CommandMSTPING, dmrd.CommandRPTPONG},
		{"MMDVM", "MMDVM", dmrd.DialectMMDVM, dmrd.CommandRPTPING, dmrd.CommandMSTPONG},
		{"record from before dialects", "", dmrd.DialectMMDVM, dmrd.CommandRPTPING, dmrd.CommandMSTPONG},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newDialectRig(t, 311865)
			check, err := secretCheck(testPassword)
			require.NoError(t, err)
			store := func() {
				r.kv.StoreRepeater(context.Background(), 311865, models.Repeater{
					RepeaterConfiguration: models.RepeaterConfiguration{ID: 311865},
					Connection:            models.RepeaterStateConnected,
					IP:                    r.from.IP.String(),
					Port:                  r.from.Port,
					LastPing:              time.Now(),
					Dialect:               tc.stored,
					SecretCheck:           check,
				})
			}
			store()

			r.control(tc.dialect, dmrd.Control{Command: tc.ping, ID: 311865})
			r.expect(tc.dialect, tc.pong)
			l, _, d := r.link(r.s)
			require.NotNil(t, l)
			assert.Equal(t, tc.dialect, d)

			end := func() {
				r.s.closeSessions(context.Background())
				r.expect(tc.dialect, dmrd.CommandMSTCL)
				r.handled(r.s)
			}
			end()
			store()
			addr, ok := r.s.resumeForEgress(context.Background(), 311865)
			require.True(t, ok)
			assert.Equal(t, r.from.String(), addr.String())
			l, _, d = r.link(r.s)
			require.NotNil(t, l)
			assert.Equal(t, tc.dialect, d)
			r.control(tc.dialect, dmrd.Control{Command: tc.ping, ID: 311865})
			r.expect(tc.dialect, tc.pong)

			end()
			b := r.replica(r.store)
			store()
			r.sendTo(b, (&models.Packet{Signature: "DMRD", Repeater: 311865, Dst: 1, GroupCall: true}).Encode())
			r.handled(b)
			l, st, d := r.link(b)
			require.NotNil(t, l, "a DMRD should resume the session")
			assert.Equal(t, tc.dialect, d)
			assert.Equal(t, dmrdsession.StateRunning, st)
		})
	}
}

// TestRPTOFromHBRPIsIgnored sends RPTO, which HBRP does not have, from an
// HBRP repeater in both ID forms. The session must go on, unanswered.
func TestRPTOFromHBRPIsIgnored(t *testing.T) {
	t.Parallel()
	r := newDialectRig(t, 311866)
	r.login(dmrd.DialectHBRP, 311866)
	bin, err := (&dmrd.Control{Command: dmrd.CommandRPTO, ID: 311866, Text: []byte("TS1=91;")}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)
	r.send(bin)
	r.expectNothing()
	r.send([]byte("RPTO0004C232TS1=91;"))
	r.expectNothing()
	_, st, _ := r.link(r.s)
	assert.Equal(t, dmrdsession.StateRunning, st)
	r.control(dmrd.DialectHBRP, dmrd.Control{Command: dmrd.CommandMSTPING, ID: 311866})
	r.expect(dmrd.DialectHBRP, dmrd.CommandRPTPONG)
}
