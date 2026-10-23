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
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	optionsRepeater = 311870
	otherRepeater   = 311871
)

type optionsRig struct {
	t    *testing.T
	s    *Server
	db   *gorm.DB
	peer *net.UDPConn
	from net.UDPAddr
}

func newOptionsRig(t *testing.T) *optionsRig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	h := hub.NewHub(database, kvStore, ps, calltracker.NewCallTracker(database, ps))
	t.Cleanup(func() {
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
	})
	for _, id := range []uint{optionsRepeater, otherRepeater} {
		require.NoError(t, database.Create(&models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: id},
			Type:                  models.RepeaterTypeMMDVM,
			Password:              "s3cret",
		}).Error)
	}
	for _, tg := range []uint{1, 2, 3, 91, 262, 3100} {
		require.NoError(t, database.Create(&models.Talkgroup{ID: tg, Name: fmt.Sprintf("TG%d", tg)}).Error)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	return &optionsRig{
		t:  t,
		db: database,
		s: &Server{
			DB:        database,
			Server:    conn,
			kvClient:  servers.MakeKVClient(kvStore),
			hub:       h,
			connected: xsync.NewMap[uint, net.UDPAddr](),
			newSalt:   func() (uint32, error) { return 0x01020304, nil },
		},
		peer: peer,
		from: *peer.LocalAddr().(*net.UDPAddr), //nolint:forcetypeassert
	}
}

func (r *optionsRig) read() dmrd.Command {
	r.t.Helper()
	buf := make([]byte, 512)
	require.NoError(r.t, r.peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, _, err := r.peer.ReadFromUDP(buf)
	require.NoError(r.t, err)
	return dmrd.Classify(buf[:n], dmrd.DialectMMDVM)
}

func (r *optionsRig) send(p []byte) {
	r.s.handlePacket(context.Background(), r.from, p)
}

func (r *optionsRig) login(id uint32) {
	r.t.Helper()
	rptl, err := (&dmrd.Control{Command: dmrd.CommandRPTL, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(r.t, err)
	r.send(rptl)
	require.Equal(r.t, dmrd.CommandRPTACK, r.read())
	token, err := dmrdauth.Token(dmrd.DialectMMDVM, 0x01020304, []byte("s3cret"))
	require.NoError(r.t, err)
	rptk, err := (&dmrd.Control{Command: dmrd.CommandRPTK, ID: id, Token: token}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(r.t, err)
	r.send(rptk)
	require.Equal(r.t, dmrd.CommandRPTACK, r.read())
	var rptc dmrd.MMDVMConfig
	rptc.Reset()
	rptc.SetID(id)
	require.NoError(r.t, errors.Join(
		rptc.SetCallsign([]byte("N0CALL")),
		rptc.SetRXFrequency(440000000),
		rptc.SetTXFrequency(440000000),
		rptc.SetPower(10),
		rptc.SetColorCode(1),
		rptc.SetLatitude([]byte("+00.0000")),
		rptc.SetLongitude([]byte("+000.0000")),
		rptc.SetHeight(10),
		rptc.SetSlots(3),
	))
	r.send(rptc[:])
	require.Equal(r.t, dmrd.CommandRPTACK, r.read())
	require.Equal(r.t, dmrdsession.StateRunning, r.s.links[r.from.String()].m.State())
}

func (r *optionsRig) options(id uint32, text string) dmrd.Command {
	r.t.Helper()
	rpto, err := (&dmrd.Control{Command: dmrd.CommandRPTO, ID: id, Text: []byte(text)}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(r.t, err)
	r.send(rpto)
	return r.read()
}

func (r *optionsRig) statics(id uint) (ts1, ts2 []uint) {
	r.t.Helper()
	rpt, err := models.FindRepeaterByID(r.db, id)
	require.NoError(r.t, err)
	ids := func(tgs []models.Talkgroup) []uint {
		out := make([]uint, 0, len(tgs))
		for _, tg := range tgs {
			out = append(out, tg.ID)
		}
		return out
	}
	return ids(rpt.TS1StaticTalkgroups), ids(rpt.TS2StaticTalkgroups)
}

func (r *optionsRig) setStatics(id uint, ts1, ts2 []uint) {
	r.t.Helper()
	rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}}
	tgs := func(ids []uint) []models.Talkgroup {
		out := make([]models.Talkgroup, 0, len(ids))
		for _, id := range ids {
			out = append(out, models.Talkgroup{ID: id})
		}
		return out
	}
	require.NoError(r.t, r.db.Model(&rpt).Association("TS1StaticTalkgroups").Replace(tgs(ts1)))
	require.NoError(r.t, r.db.Model(&rpt).Association("TS2StaticTalkgroups").Replace(tgs(ts2)))
}

func TestOptionsSetStaticTalkgroups(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		before   [2][]uint
		options  []string
		ts1, ts2 []uint
	}{
		{name: "freedmr", options: []string{"TS1=1,2;TS2=91;DIAL=0;VOICE=1;TIMER=10;"}, ts1: []uint{1, 2}, ts2: []uint{91}},
		{name: "freedmr long keys", options: []string{"TS1_STATIC=3;TS2_STATIC=1,91;"}, ts1: []uint{3}, ts2: []uint{1, 91}},
		{name: "dmrplus", before: [2][]uint{{3}, {91}}, options: []string{"StartRef=4013;RelinkTime=15;UserLink=1;TS1_1=262;TS1_2=1;"}, ts1: []uint{1, 262}, ts2: []uint{}},
		{name: "dmrplus both slots", options: []string{"TS1_1=1;TS1_2=2;TS2_1=91;TS2_2=3100;"}, ts1: []uint{1, 2}, ts2: []uint{91, 3100}},
		{name: "repeated replaces", options: []string{"TS1=1,2;TS2=91;", "TS1=3;TS2=3100;"}, ts1: []uint{3}, ts2: []uint{3100}},
		{name: "empty clears", before: [2][]uint{{1}, {91}}, options: []string{"TS1=2;TS2=;"}, ts1: []uint{2}, ts2: []uint{}},
		{name: "absent clears", before: [2][]uint{{1}, {91}}, options: []string{"TS2=3;"}, ts1: []uint{}, ts2: []uint{3}},
		{name: "absent clears dmrplus", before: [2][]uint{{1}, {91}}, options: []string{"TS2_1=3;"}, ts1: []uint{}, ts2: []uint{3}},
		{name: "unknown talkgroups left out", before: [2][]uint{{1}, {91}}, options: []string{"TS1=2,4444,3;TS2=3;"}, ts1: []uint{2, 3}, ts2: []uint{3}},
		{name: "only unknown talkgroups keep slot", before: [2][]uint{{1}, {91}}, options: []string{"TS1=4444,5555;TS2=3;"}, ts1: []uint{1}, ts2: []uint{3}},
		{name: "only unknown talkgroups keep slot absent other clears", before: [2][]uint{{1}, {91}}, options: []string{"TS1_1=4444;"}, ts1: []uint{1}, ts2: []uint{}},
		{name: "duplicates once", options: []string{"TS1=1,1,2;"}, ts1: []uint{1, 2}, ts2: []uint{}},
		{name: "malformed list keeps slot", before: [2][]uint{{1}, {91}}, options: []string{"TS1=2,abc;TS2=3;"}, ts1: []uint{1}, ts2: []uint{3}},
		{name: "garbage ignored", before: [2][]uint{{1}, {91}}, options: []string{"not options at all", "\x00\xff\x01", ""}, ts1: []uint{1}, ts2: []uint{91}},
		{name: "unknown keys ignored", before: [2][]uint{{1}, {91}}, options: []string{"Type=HBlink;DIAL=4000;SINGLE=1;"}, ts1: []uint{1}, ts2: []uint{91}},
		{name: "overlength ignored", before: [2][]uint{{1}, {91}}, options: []string{"TS1=2;TS2=3;" + strings.Repeat("X", dmrd.MaxFreeDMROptionsLen)}, ts1: []uint{1}, ts2: []uint{91}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newOptionsRig(t)
			r.setStatics(optionsRepeater, tc.before[0], tc.before[1])
			r.setStatics(otherRepeater, []uint{1}, []uint{91})
			r.login(optionsRepeater)
			for _, o := range tc.options {
				require.Equal(t, dmrd.CommandRPTACK, r.options(optionsRepeater, o), "options %q", o)
			}
			ts1, ts2 := r.statics(optionsRepeater)
			require.ElementsMatch(t, tc.ts1, ts1, "TS1")
			require.ElementsMatch(t, tc.ts2, ts2, "TS2")
			rpt, err := models.FindRepeaterByID(r.db, optionsRepeater)
			require.NoError(t, err)
			require.Equal(t, "s3cret", rpt.Password, "the options must leave the other fields of the repeater")
			require.Equal(t, "N0CALL", rpt.Callsign, "the options must leave the other fields of the repeater")
			require.Equal(t, models.RepeaterTypeMMDVM, rpt.Type, "the options must leave the other fields of the repeater")
			ts1, ts2 = r.statics(otherRepeater)
			require.Equal(t, []uint{1}, ts1, "the options must not change another repeater")
			require.Equal(t, []uint{91}, ts2, "the options must not change another repeater")
			require.Equal(t, dmrdsession.StateRunning, r.s.links[r.from.String()].m.State(), "options must not end the session")
		})
	}
}

func TestOptionsForAnotherRepeaterRejected(t *testing.T) {
	t.Parallel()
	r := newOptionsRig(t)
	r.setStatics(optionsRepeater, []uint{1}, []uint{91})
	r.setStatics(otherRepeater, []uint{1}, []uint{91})
	r.login(optionsRepeater)

	require.Equal(t, dmrd.CommandMSTNAK, r.options(otherRepeater, "TS1=2;TS2=3;"))
	for _, id := range []uint{optionsRepeater, otherRepeater} {
		ts1, ts2 := r.statics(id)
		require.Equal(t, []uint{1}, ts1)
		require.Equal(t, []uint{91}, ts2)
	}

	l := r.s.links[r.from.String()]
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	r.s.run(context.Background(), r.s.now(), l, append(buf[:0], dmrdsession.Output{
		Kind: dmrdsession.OutputOptions, ID: otherRepeater, Packet: []byte("TS1=2;TS2=3;"),
	}))
	for _, id := range []uint{optionsRepeater, otherRepeater} {
		ts1, ts2 := r.statics(id)
		require.Equal(t, []uint{1}, ts1, "an options output for another repeater must not apply")
		require.Equal(t, []uint{91}, ts2, "an options output for another repeater must not apply")
	}
}

func (r *optionsRig) setDynamic(id uint, ts1, ts2 uint) {
	r.t.Helper()
	ptr := func(v uint) *uint {
		if v == 0 {
			return nil
		}
		return &v
	}
	require.NoError(r.t, r.db.Model(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}}).
		Select("TS1DynamicTalkgroupID", "TS2DynamicTalkgroupID").
		Updates(map[string]any{"TS1DynamicTalkgroupID": ptr(ts1), "TS2DynamicTalkgroupID": ptr(ts2)}).Error)
}

func (r *optionsRig) dynamic(id uint) (ts1, ts2 uint) {
	r.t.Helper()
	rpt, err := models.FindRepeaterByID(r.db, id)
	require.NoError(r.t, err)
	if rpt.TS1DynamicTalkgroupID != nil {
		ts1 = *rpt.TS1DynamicTalkgroupID
	}
	if rpt.TS2DynamicTalkgroupID != nil {
		ts2 = *rpt.TS2DynamicTalkgroupID
	}
	return ts1, ts2
}

func TestOptionsDialLinksDynamicTalkgroup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		before   [2]uint
		options  []string
		ts1, ts2 uint
	}{
		{name: "dial", options: []string{"DIAL=91;"}, ts2: 91},
		{name: "dial with statics", options: []string{"TS1=1;TS2=2;DIAL=91;VOICE=1;"}, ts2: 91},
		{name: "dial per slot", options: []string{"DIAL1=3100;DIAL2=91;"}, ts1: 3100, ts2: 91},
		{name: "default dial keys", options: []string{"default_dial_ts1=3100;default_dial_ts2=262;"}, ts1: 3100, ts2: 262},
		{name: "default reflector", options: []string{"DEFAULT_REFLECTOR=262;"}, ts2: 262},
		{name: "dmrplus start reflector", options: []string{"StartRef=262;RelinkTime=15;UserLink=1;TS1_1=1;"}, ts2: 262},
		{name: "dial replaces a link", before: [2]uint{1, 2}, options: []string{"DIAL1=3100;DIAL=91;"}, ts1: 3100, ts2: 91},
		{name: "repeated dial relinks", options: []string{"DIAL=91;", "DIAL=262;"}, ts2: 262},
		{name: "dial zero unlinks", before: [2]uint{1, 2}, options: []string{"DIAL=0;"}, ts1: 1},
		{name: "empty dial unlinks", before: [2]uint{1, 2}, options: []string{"DIAL1=;DIAL2=;"}},
		{name: "prohibited dial unlinks", before: [2]uint{1, 2}, options: []string{"DIAL=9990;DIAL1=9;"}},
		{name: "unknown talkgroup ignored", before: [2]uint{1, 2}, options: []string{"DIAL=4444;"}, ts1: 1, ts2: 2},
		{name: "absent dial keeps", before: [2]uint{1, 2}, options: []string{"TS1=3;"}, ts1: 1, ts2: 2},
		{name: "malformed dial keeps", before: [2]uint{1, 2}, options: []string{"DIAL=abc;"}, ts1: 1, ts2: 2},
		{name: "overlength ignored", before: [2]uint{1, 2}, options: []string{"DIAL=91;" + strings.Repeat("X", dmrd.MaxFreeDMROptionsLen)}, ts1: 1, ts2: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newOptionsRig(t)
			r.setDynamic(optionsRepeater, tc.before[0], tc.before[1])
			r.setDynamic(otherRepeater, 1, 2)
			r.login(optionsRepeater)
			for _, o := range tc.options {
				require.Equal(t, dmrd.CommandRPTACK, r.options(optionsRepeater, o), "options %q", o)
			}
			require.Eventually(t, func() bool {
				ts1, ts2 := r.dynamic(optionsRepeater)
				return ts1 == tc.ts1 && ts2 == tc.ts2
			}, 2*time.Second, 10*time.Millisecond, "dynamic talkgroups should become TS1 %d TS2 %d", tc.ts1, tc.ts2)
			ts1, ts2 := r.dynamic(otherRepeater)
			require.Equal(t, uint(1), ts1, "the options must not change another repeater")
			require.Equal(t, uint(2), ts2, "the options must not change another repeater")
		})
	}
}
