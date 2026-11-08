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
	"bytes"
	"context"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	internalhttp "github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// sinks keeps everything that a stack wrote to its KV store and published,
// and every log line.
type sinks struct {
	mu   sync.Mutex
	logs bytes.Buffer
	kv   [][]byte
	pub  [][]byte
}

func (s *sinks) add(to *[][]byte, parts ...[]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range parts {
		*to = append(*to, bytes.Clone(p))
	}
}

func (s *sinks) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.logs.Write(p)
	if err != nil {
		return n, fmt.Errorf("keep a log line: %w", err)
	}
	return n, nil
}

func (s *sinks) logText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs.String()
}

type recordingKV struct {
	kv.KV
	s *sinks
}

func (r recordingKV) Set(ctx context.Context, key string, value []byte) error {
	r.s.add(&r.s.kv, []byte(key), value)
	return r.KV.Set(ctx, key, value) //nolint:wrapcheck
}

func (r recordingKV) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	r.s.add(&r.s.kv, []byte(key), []byte(value))
	return r.KV.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

func (r recordingKV) RPush(ctx context.Context, key string, value []byte) (int64, error) {
	r.s.add(&r.s.kv, []byte(key), value)
	return r.KV.RPush(ctx, key, value) //nolint:wrapcheck
}

func (r recordingKV) RPushTrim(ctx context.Context, key string, value []byte, keep int64, ttl time.Duration) error {
	r.s.add(&r.s.kv, []byte(key), value)
	return r.KV.RPushTrim(ctx, key, value, keep, ttl) //nolint:wrapcheck
}

func (r recordingKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	r.s.add(&r.s.kv, []byte(key))
	return r.KV.Update(ctx, key, ttl, func(old []byte) ([]byte, error) { //nolint:wrapcheck
		v, err := fn(old)
		if err == nil {
			r.s.add(&r.s.kv, v)
		}
		return v, err
	})
}

type recordingPubSub struct {
	pubsub.PubSub
	s *sinks
}

func (r recordingPubSub) Publish(topic string, message []byte) error {
	r.s.add(&r.s.pub, []byte(topic), message)
	return r.PubSub.Publish(topic, message) //nolint:wrapcheck
}

// codeNeedles returns the patterns that find the code, or the call to it,
// in text: the call ID as a whole number and in hex, and the code next to
// a word that names it. The code alone is not a needle: random times and
// durations hold 6-digit numbers.
func codeNeedles(code uint32) []*regexp.Regexp {
	dst := dmrconst.UnlockFirst + uint(code)
	return []*regexp.Regexp{
		regexp.MustCompile(`(^|[^0-9])` + strconv.FormatUint(uint64(dst), 10) + `($|[^0-9])`),
		regexp.MustCompile(fmt.Sprintf(`(?i)(^|[^0-9a-f])(0x)?0*%x($|[^0-9a-f])`, dst)),
		regexp.MustCompile(fmt.Sprintf(`(?i)(code|otp|pin|digits)[^0-9]{0,16}0*%d($|[^0-9])`, code)),
	}
}

// leaks returns where the code is in b: as text, or as the 32-bit number
// of the call ID that msgpack and binary forms carry. With bare, the code
// as a whole number is a needle too.
func leaks(code uint32, b []byte, bare bool) []string {
	needles := codeNeedles(code)
	if bare {
		needles = append(needles, regexp.MustCompile(fmt.Sprintf(`(^|[^0-9])%06d($|[^0-9])`, code)))
	}
	var found []string
	for _, n := range needles {
		if loc := n.FindIndex(b); loc != nil {
			found = append(found, string(b[max(0, loc[0]-80):min(len(b), loc[1]+80)]))
		}
	}
	var bin [4]byte
	binary.BigEndian.PutUint32(bin[:], uint32(dmrconst.UnlockFirst)+code)
	if i := bytes.Index(b, bin[:]); i >= 0 {
		found = append(found, fmt.Sprintf("the call ID as binary at %d", i))
	}
	return found
}

// sendText sends a TMS text from src to dst on the slot of the hotspot of
// c, slot 2 when slot2 is true.
func sendText(t *testing.T, c *testutils.MMDVMClient, slot2 bool, src, dst uint, body string, stream uint) {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatTMS, Source: uint32(src), Destination: uint32(dst), Body: []byte(body)}, &h) //nolint:gosec // DMR IDs fit 24 bits
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 7, layer2.SyncSourceMS)
	require.NoError(t, err)
	pkts := make([]models.Packet, 0, len(bursts))
	for i, raw := range bursts {
		pkts = append(pkts, dataPacket(call{src: uint32(src), dst: uint32(dst)}, slot2, stream, i, raw)) //nolint:gosec // DMR IDs fit 24 bits
	}
	sendPackets(t, c.SendDMRD, pkts)
}

// testCode returns a code of secret that a try at now takes, other than the
// codes whose call IDs other tests use.
func testCode(secret []byte, now time.Time) uint32 {
	step := airauth.Step(now)
	c := airauth.Code(secret, step+1)
	if c == 0 || c == 999999 {
		c = airauth.Code(secret, step)
	}
	return c
}

type unlockTries struct{ results chan airauth.Result }

func (u unlockTries) Locked(hub.Unlock) {}

func (u unlockTries) Tried(_ hub.Unlock, r airauth.Result) { u.results <- r }

const (
	codeOwner     = uint(1000001)
	codeRadio     = uint(1000101)
	codeBystander = uint(1000002)
	codeMMDVM     = uint(100001)
	codeIPSC      = uint(200001)
)

// TestAnOnAirCodeReachesNoSink sends calls to on-air codes through an
// MMDVM hotspot and an IPSC repeater, the right code, a wrong one and one
// from a radio ID with no owner, and then walks every place that the hub
// keeps or sends data: every table of the database, among them the calls
// and the connection logs, every write to the KV store, every publish,
// the texts that store and forward holds and sends, every log line at the
// debug level, the metrics, and the last heard,
// on-air and connection log APIs while a call to a code is open. The IPSC
// and MMDVM paths cover the logs of the IPSC translator and of the MMDVM
// server, and TestAnRFCallToAnOnAirCodeLeavesTheCodeOutOfTheLog covers the
// logs of the modem. None may hold the code or the call ID that carries it.
func TestAnOnAirCodeReachesNoSink(t *testing.T) { //nolint:paralleltest,tparallel // it swaps the default logger, which its backends share
	shared := &sinks{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(shared, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			own := &sinks{}
			be.WrapKV = func(k kv.KV) kv.KV { return recordingKV{KV: k, s: own} }
			be.WrapPubSub = func(p pubsub.PubSub) pubsub.PubSub { return recordingPubSub{PubSub: p, s: own} }
			stack := testutils.SetupIntegrationStack(t, be)
			stack.ChangeConfig(func(c *config.Config) { c.Secret = "a-secret-of-fifteen" })
			stack.Hub.SetConfig(stack.Current)
			tries := unlockTries{results: make(chan airauth.Result, 8)}
			stack.Hub.SetUnlockReplies(tries)

			stack.SeedUser(t, codeOwner, "N0CODE")
			stack.SeedUser(t, codeBystander, "N0BYST")
			require.NoError(t, stack.DB.Create(&models.UserRadioID{UserID: codeOwner, RadioID: codeRadio, VerifiedAt: time.Now()}).Error)
			stack.Hub.RadioChanged(codeRadio)
			stack.SeedTalkgroup(t, 1, "TG1")
			stack.SeedMMDVMRepeater(t, codeMMDVM, codeOwner, testPassword)
			stack.SeedIPSCRepeater(t, codeIPSC, codeOwner, ipscAuthKey)
			stack.AssignTS1StaticTG(t, codeMMDVM, 1)
			stack.AssignTS1StaticTG(t, codeIPSC, 1)

			ctx := context.Background()
			now := time.Now()
			stack.Hub.SetUnlockClock(clock.NewFake(now))
			setup, err := stack.Hub.OnAirCodes().Begin(ctx, codeOwner, "DMRHub", "n0code")
			require.NoError(t, err)
			secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
			require.NoError(t, err)
			require.NoError(t, stack.Hub.OnAirCodes().Confirm(ctx, codeOwner, airauth.Code(secret, airauth.Step(now)-1)))
			stack.Hub.UserChanged(codeOwner)

			stack.StartServers(t)
			mc := testutils.NewMMDVMClient(uint32(codeMMDVM), "", testPassword)
			require.NoError(t, mc.Connect(stack.MMDVMAddr))
			defer mc.Close()
			require.NoError(t, mc.WaitReady(handshakeWait))
			ic := testutils.NewIPSCClient(uint32(codeIPSC), ipscAuthKey)
			require.NoError(t, ic.Connect(stack.IPSCAddr))
			defer ic.Close()
			require.NoError(t, ic.WaitReady(handshakeWait))

			require.NoError(t, models.SetStoreForward(stack.DB, codeOwner, true, time.Now()))
			sendText(t, mc, true, codeBystander, codeRadio, "held for the radio of the code", 9010)
			waitFor(t, "the text to the radio to be held", func() bool {
				var n int64
				return stack.DB.Model(&models.HeldMessage{}).Count(&n).Error == nil && n > 0
			})

			code := testCode(secret, now)
			wrong := (code + 500000) % 1000000
			if wrong == 0 || wrong == 999999 {
				wrong = 123457
			}
			call := func(src uint, c uint32, stream uint) {
				for _, p := range []models.Packet{makePrivateVoicePacket(dmrconst.UnlockFirst+uint(c), stream), makePrivateVoiceTermPacket(dmrconst.UnlockFirst+uint(c), stream)} {
					p.Src = src
					require.NoError(t, mc.SendDMRD(p))
				}
			}
			result := func() airauth.Result {
				got := testutils.Collect(tries.results, deliverWait, gotN[airauth.Result](1))
				require.Len(t, got, 1, "a try of a code did not end")
				return got[0]
			}
			call(codeRadio, code, 9001)
			require.Equal(t, airauth.Granted, result())
			require.NoError(t, ic.SendPrivateVoice(codeRadio, dmrconst.UnlockFirst+uint(wrong), false, 9002))
			require.Equal(t, airauth.Refused, result())
			call(3002401, code, 9003)
			require.Equal(t, airauth.Refused, result())
			sendText(t, mc, false, codeRadio, dmrconst.UnlockFirst+uint(code), "a text to a code", 9011)

			require.NoError(t, mc.SendDMRD(makeGroupVoicePacket(codeRadio, 1, 9004, false)))
			require.NoError(t, mc.SendDMRD(makeGroupVoiceTermPacket(codeRadio, 1, 9004, false)))
			assert.NotEmpty(t, receiveIPSCCall(ic, 1), "the right code must unlock the radio on its hotspot slot")
			require.NoError(t, ic.SendGroupVoice(codeBystander, 1, false, 9005))
			assert.NotEmpty(t, receiveCall(mc, 1))
			sendText(t, mc, false, codeRadio, codeBystander, "a text that shows the format of the radio", 9012)
			waitFor(t, "store and forward to send the held text to the unlocked radio", func() bool {
				stack.StoreForward.Flush()
				var n int64
				return stack.DB.Model(&models.HeldMessage{}).Where("radio_id = ? AND state <> ?", codeRadio, models.HeldStateHeld).Count(&n).Error == nil && n > 0
			})
			stack.CallTracker.Flush()
			stack.MessageStore.Flush()
			stack.StoreForward.Flush()

			ready := &atomic.Bool{}
			ready.Store(true)
			router := internalhttp.CreateRouter(ctx, stack.Current.Get(), stack.Hub, stack.DB, stack.PubSub, ready, nil, "test", "deadbeef", internalhttp.WithKV(stack.KV))
			admin := testutils.SeedAccount(t, stack.DB, 1000009, "N0ADMN", "n0admn")
			require.NoError(t, stack.DB.Model(&models.User{}).Where("id = ?", admin.ID).Update("admin", true).Error)
			jar := testutils.Login(t, router, "n0admn")
			waitFor(t, "the connection log of the hotspot", func() bool {
				w := testutils.Do(t, router, jar, http.MethodGet, fmt.Sprintf("/api/v1/repeaters/%d/events", codeMMDVM), nil)
				return w.Code == http.StatusOK && strings.Contains(w.Body.String(), "connected")
			})

			open := makePrivateVoicePacket(dmrconst.UnlockFirst+uint(code), 9006)
			open.Src = codeRadio
			require.NoError(t, mc.SendDMRD(open))
			require.Equal(t, airauth.Refused, result(), "a used code must not work again")
			require.NoError(t, mc.SendDMRD(makeGroupVoicePacket(codeRadio, 1, 9007, false)))
			waitFor(t, "the open call after the open call to a code on the hotspot", func() bool {
				w := testutils.Do(t, router, jar, http.MethodGet, fmt.Sprintf("/api/v1/repeaters/%d/on-air", codeMMDVM), nil)
				return w.Code == http.StatusOK && strings.Contains(w.Body.String(), fmt.Sprintf(`"source_id":%d`, codeRadio))
			})
			stack.CallTracker.Flush()
			api := make([]string, 0, 8)
			for _, path := range []string{
				"/api/v1/lastheard",
				fmt.Sprintf("/api/v1/lastheard/user/%d", codeOwner),
				fmt.Sprintf("/api/v1/lastheard/repeater/%d", codeMMDVM),
				fmt.Sprintf("/api/v1/lastheard/repeater/%d", codeIPSC),
				fmt.Sprintf("/api/v1/repeaters/%d/on-air", codeMMDVM),
				fmt.Sprintf("/api/v1/repeaters/%d/on-air", codeIPSC),
				fmt.Sprintf("/api/v1/repeaters/%d/events", codeMMDVM),
				fmt.Sprintf("/api/v1/repeaters/%d/events", codeIPSC),
			} {
				w := testutils.Do(t, router, jar, http.MethodGet, path, nil)
				require.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
				api = append(api, path+" "+w.Body.String())
			}
			assert.Contains(t, api[0], fmt.Sprintf(`"source_id":%d`, codeRadio), "last heard must show the open call")
			require.NoError(t, mc.SendDMRD(makePrivateVoiceTermPacket(dmrconst.UnlockFirst+uint(code), 9006)))
			require.NoError(t, mc.SendDMRD(makeGroupVoiceTermPacket(codeRadio, 1, 9007, false)))
			stack.CallTracker.Flush()

			f := &finder{codes: []uint32{code, wrong}}
			walkDB(t, stack.DB, f)
			walkRecorded(t, own, f)
			walkKV(t, ctx, stack.KV, f)
			for _, body := range api {
				f.check("the API", []byte(body), false)
			}
			logs := shared.logText()
			assert.True(t, strings.Contains(logs, fmt.Sprintf(`msg="On-air code try" src=%d result=ok`, codeRadio)), "the log keeps who tried a code")
			assert.True(t, strings.Contains(logs, "DMRD packet received"), "the log must be read at the debug level")
			f.check("the log", []byte(logs), false)
			walkMetrics(t, f)

			assert.Empty(t, f.where, "the on-air code reached these places")
		})
	}
}

// finder keeps the places where it found one of codes.
type finder struct {
	codes []uint32
	where []string
}

func (f *finder) check(place string, b []byte, bare bool) {
	for _, c := range f.codes {
		for _, l := range leaks(c, b, bare) {
			f.where = append(f.where, place+": "+l)
		}
	}
}

func walkDB(t *testing.T, db *gorm.DB, f *finder) {
	t.Helper()
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	require.Contains(t, tables, "calls")
	require.Contains(t, tables, "repeater_events")
	require.Contains(t, tables, "held_messages")
	rowsSeen := 0
	for _, table := range tables {
		rows, err := db.Table(table).Rows()
		require.NoError(t, err, table)
		cols, err := rows.Columns()
		require.NoError(t, err)
		for rows.Next() {
			rowsSeen++
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			require.NoError(t, rows.Scan(ptrs...))
			for i, v := range vals {
				b, ok := v.([]byte)
				if !ok {
					b = fmt.Appendf(nil, "%v", v)
				}
				f.check(fmt.Sprintf("table %s column %s", table, cols[i]), b, false)
			}
		}
		require.NoError(t, rows.Err())
		_ = rows.Close()
	}
	assert.Positive(t, rowsSeen)
}

func walkRecorded(t *testing.T, own *sinks, f *finder) {
	t.Helper()
	own.mu.Lock()
	defer own.mu.Unlock()
	assert.NotEmpty(t, own.kv)
	assert.Contains(t, own.pub, []byte("calls"), "the calls after the codes must be published")
	for _, w := range own.kv {
		f.check("a write to the KV store", w, false)
	}
	for _, p := range own.pub {
		f.check("a publish", p, false)
	}
}

func walkKV(t *testing.T, ctx context.Context, store kv.KV, f *finder) {
	t.Helper()
	keys, _, err := store.Scan(ctx, 0, "*", 100000)
	require.NoError(t, err)
	for _, k := range keys {
		v, err := store.Get(ctx, k)
		if err != nil {
			continue
		}
		f.check("the KV store", append([]byte(k+" "), v...), strings.HasPrefix(k, "dmrhub:airauth:"))
	}
}

func walkMetrics(t *testing.T, f *finder) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		for _, m := range fam.GetMetric() {
			for _, lp := range m.GetLabel() {
				f.check("the metric "+fam.GetName(), []byte(lp.GetValue()), false)
			}
		}
	}
}
