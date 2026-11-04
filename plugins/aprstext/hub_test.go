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

package aprstext_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/aprs/aprstest"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugintexts"
	"github.com/USA-RedDragon/DMRHub/internal/positions"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/DMRHub/plugins/aprstext"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	// n4call is a user whose radio sends Defined Data texts.
	n4call = 1000004
	// n3call is a user who leaves the switch off.
	n3call = 1000003
	// n1Hotspot and n4Hotspot are the hotspots of N1CALL and N4CALL.
	n1Hotspot = 311000101
	n4Hotspot = 311000401
	// passcode is the APRS-IS passcode of K0CALL, the login of the hub.
	passcode = "14303"
	hubWait  = time.Minute
)

func forAllBackends(t *testing.T, f func(t *testing.T, be testutils.Backend)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			f(t, be)
		})
	}
}

// sentData collects the texts that the hub sends to radios.
type sentData struct {
	mu   sync.Mutex
	got  []data.Message
	more chan struct{}
}

func (s *sentData) SendData(_ context.Context, _ uint, _ bool, m data.Message) (data.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, m)
	select {
	case s.more <- struct{}{}:
	default:
	}
	return data.ResultSent, nil
}

func (s *sentData) all() []data.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

type rig struct {
	t      *testing.T
	cfg    config.Config
	db     *gorm.DB
	ps     pubsub.PubSub
	kv     kv.KV
	srv    *aprstest.Server
	out    *sentData
	plugin models.Plugin
	// switches gets the users of each UserSwitches that the plugin got.
	switches chan []string
}

func newRig(t *testing.T, be testutils.Backend) *rig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	be.Migrated().Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	h := hub.NewHub(database, kvStore, ps, ct)
	t.Cleanup(func() {
		h.Stop(context.Background())
		ct.Stop()
		_ = ps.Close()
		_ = kvStore.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	srv := aprstest.NewServer(t, passcode)
	require.True(t, strings.HasPrefix(srv.Addr(), "127.0.0.1:"), "tests send only to loopback")
	cfg.DMR.Positions.APRSIS = config.APRSIS{Callsign: "K0CALL-10", Server: srv.Addr()}
	for _, u := range []models.User{
		{ID: userID, Callsign: "N1CALL", Username: "n1call", Approved: true},
		{ID: n3call, Callsign: "N3CALL", Username: "n3call", Approved: true},
		{ID: n4call, Callsign: "N4CALL", Username: "n4call", Approved: true},
	} {
		require.NoError(t, database.Create(&u).Error)
	}
	for id, owner := range map[uint]uint{n1Hotspot: userID, n4Hotspot: n4call} {
		require.NoError(t, database.Create(&models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: id, Callsign: "N0CALL"}, OwnerID: owner, Type: models.RepeaterTypeMMDVM,
		}).Error)
	}
	r := &rig{t: t, cfg: cfg, db: database, ps: ps, kv: kvStore, srv: srv, out: &sentData{more: make(chan struct{}, 1)}, switches: make(chan []string, 16)}
	listener := testutils.StartPlugins(t, database, ps, h, func(reg *plugins.Registry) { h.SetPlugins(reg) }, nil,
		plugins.WithAPRS(plugins.APRS{
			Send:   func(m aprs.Message) error { return positions.PublishMessage(ps, m) },
			On:     func() bool { return true },
			Limits: kvStore,
		}), plugins.WithAPRSInbox(), plugins.WithTexts(plugintexts.New(database, r.out)))
	r.plugin = listener.Run(t, "APRS messages", []models.PluginRange{{First: service, Last: service}}, r.serve)
	assert.Empty(t, r.nextSwitches(), "the users before any turned the switch on")
	r.declare()
	return r
}

// serve is Serve, which also tells the test the callsigns of each
// UserSwitches. The hub sends one after each Subscribe, once it took the
// subscription.
func (r *rig) serve(ctx context.Context, conn *plugin.Conn) error {
	s := aprstext.New(ctx, conn, conn.Ranges())
	defer s.Stop()
	if err := s.Start(); err != nil {
		return fmt.Errorf("could not subscribe: %w", err)
	}
	for {
		m, err := conn.Recv()
		if err != nil {
			return fmt.Errorf("could not receive: %w", err)
		}
		s.Handle(m) //nolint:contextcheck // the service works under the context of its connection
		if us := m.GetUserSwitches(); us != nil {
			var calls []string
			for _, sw := range us.GetSwitches() {
				calls = append(calls, sw.GetCallsign())
			}
			r.switches <- calls
		}
	}
}

func next[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), hubWait)
	defer cancel()
	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatalf("no %s within %s", what, hubWait)
		var zero T
		return zero
	}
}

func (r *rig) nextSwitches() []string {
	r.t.Helper()
	return next(r.t, r.switches, "users of the switch")
}

// declare stores the switches of the plugin's Hello, as the hub does when
// the plugin says hello.
func (r *rig) declare() {
	r.t.Helper()
	switches := aprstext.Hello().GetSwitches()
	specs := make([]models.PluginSwitchSpec, 0, len(switches))
	for _, sw := range switches {
		spec := models.PluginSwitchSpec{Key: sw.GetKey(), Title: sw.GetTitle(), Description: sw.GetDescription(), Notice: sw.GetNotice(),
			Choice: &models.PluginSwitchChoice{Label: sw.GetChoice().GetLabel(), Options: sw.GetChoice().GetOptions(), Preset: sw.GetChoice().GetPreset()}}
		for _, d := range sw.GetDetails() {
			spec.Details = append(spec.Details, models.PluginSwitchDetail{Label: d.GetLabel(), Value: d.GetValue()})
		}
		require.NoError(r.t, spec.Check())
		specs = append(specs, spec)
	}
	require.NoError(r.t, models.SetPluginSwitches(r.db, r.plugin.ID, specs))
}

// turnOn turns the switch of user on with ssid, as Settings does, and
// waits until the plugin subscribed to the addresses of calls, the
// callsigns whose switches are on then.
func (r *rig) turnOn(user uint, ssid string, calls ...string) {
	r.t.Helper()
	u, err := models.SetUserPluginSwitch(r.db, r.plugin.ID, user, aprstext.SwitchKey, true, ssid, time.Now())
	require.NoError(r.t, err)
	require.NoError(r.t, plugins.PublishSwitch(r.ps, r.plugin.ID, &u))
	got := r.nextSwitches()
	for !slices.Equal(got, calls) {
		got = r.nextSwitches()
	}
}

// gateway starts the APRS-IS login of the hub with the inbox of
// cmd/root.go, and waits until the server verified it.
func (r *rig) gateway() {
	r.t.Helper()
	gw := positions.NewGateway(r.db, &r.cfg, r.kv, r.ps, "1.2.3")
	gw.Inbox(func(context.Context) ([]string, error) { return models.PluginSwitchCallsigns(r.db) }, func(m aprs.Message) {
		assert.NoError(r.t, plugins.PublishAPRS(r.ps, &pluginv1.APRSMessage{Source: m.Source, Addressee: m.Addressee, Text: m.Text, Number: m.Number}))
	})
	gw.Start(context.Background())
	r.t.Cleanup(gw.Stop)
	ctx, cancel := context.WithTimeout(r.t.Context(), hubWait)
	defer cancel()
	require.Equal(r.t, 1, r.srv.WaitClients(ctx, 1), "the gateway did not log in")
}

// lines waits until the hub sent n lines to APRS-IS and returns them.
func (r *rig) lines(n int) []string {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.t.Context(), hubWait)
	defer cancel()
	got := r.srv.WaitLines(ctx, n)
	require.Len(r.t, got, n)
	return got
}

// texts waits until the hub sent n texts to radios and returns them.
func (r *rig) texts(n int) []data.Message {
	r.t.Helper()
	for len(r.out.all()) < n {
		next(r.t, r.out.more, "text to a radio")
	}
	got := r.out.all()
	require.Len(r.t, got, n)
	return got
}

// radioSent stores a text that radio sent to the plugin through hotspot,
// whose format the hub then uses for the texts to radio, and returns it.
func (r *rig) radioSent(radio, hotspot uint, text *sms.Text, rate enums.DataType, body string) *models.Message {
	r.t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(r.t, err)
	p, err := packet.NewPacket(h.Data, payload, rate)
	require.NoError(r.t, err)
	head, err := p.Header.Encode()
	require.NoError(r.t, err)
	format := models.MessageFormatTMS
	if text.Format == sms.FormatDefinedData {
		format = models.MessageFormatDefinedData
	}
	m := &models.Message{
		ReceivedAt: time.Now(), SourceID: radio, DestinationID: service, RepeaterID: hotspot, Slot: 2, Format: format,
		ChecksumOK: true, Rate: uint8(rate), Raw: append(head[:], payload...), Text: &body,
	}
	require.NoError(r.t, r.db.Create(m).Error)
	return m
}

func decoded(t *testing.T, m data.Message) (sms.Text, string) {
	t.Helper()
	body, text, err := sms.Decode(nil, &m.Header, m.Payload)
	require.NoError(t, err)
	return text, string(body)
}

// TestAPRSMessagesGoBothWaysThroughTheHub runs the plugin on a hub with
// its APRS-IS login on the fake server. N1CALL's radio sends TMS at rate
// 3/4 and N4CALL's Defined Data in 7 bits at rate 1/2; each gets its APRS
// messages in its own format. N3CALL left the switch off.
func TestAPRSMessagesGoBothWaysThroughTheHub(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, be testutils.Backend) {
		r := newRig(t, be)
		r.radioSent(userID, n1Hotspot, &sms.Text{Format: sms.FormatTMS, Source: userID, Destination: service, Sequence: 3, Body: []byte("hi")}, enums.DataTypeRate34, "hi")
		r.radioSent(n4call, n4Hotspot, &sms.Text{Format: sms.FormatDefinedData, DD: enums.DD7Bit, Source: n4call, Destination: service, Body: []byte("hi")}, enums.DataTypeRate12, "hi")
		r.radioSent(n3call, n4Hotspot, &sms.Text{Format: sms.FormatTMS, Source: n3call, Destination: service, Body: []byte("hi")}, enums.DataTypeRate34, "hi")
		r.turnOn(userID, "9", "N1CALL")
		r.turnOn(n4call, "", "N1CALL", "N4CALL")
		r.gateway()
		assert.Equal(t, []string{"user K0CALL-10 pass " + passcode + " vers DMRHub 1.2.3 filter g/N1CALL/N1CALL-*/N4CALL/N4CALL-*"}, r.srv.Logins(),
			"the login asks APRS-IS only for the callsigns of the users who turned the switch on")

		r.srv.Push("N2CALL-7>APDR16,TCPIP*,qAC,T2TEST::N3CALL-7 :switch off{1")
		r.srv.Push("N2CALL-7>APDR16,TCPIP*,qAC,T2TEST::N1CALL-7 :other SSID{2")
		r.srv.Push("N2CALL-7>APDR16,TCPIP*,qAC,T2TEST::N1CALL-9 :See you at six{3")
		sent := r.texts(1)
		text, body := decoded(t, sent[0])
		assert.Equal(t, sms.FormatTMS, text.Format)
		assert.Equal(t, enums.DataTypeRate34, sent[0].Rate)
		assert.Equal(t, []uint32{service, userID}, []uint32{text.Source, text.Destination})
		assert.Equal(t, "N2CALL-7: See you at six", body)
		assert.Equal(t, []string{"N1CALL-9>" + aprs.ToCall + ",TCPIP*::N2CALL-7 :ack3"}, r.lines(1))

		r.srv.Push("N2CALL-7>APDR16,TCPIP*,qAC,T2TEST::N4CALL-7 :Seven bits{4")
		sent = r.texts(2)
		text, body = decoded(t, sent[1])
		assert.Equal(t, sms.FormatDefinedData, text.Format)
		assert.Equal(t, enums.DD7Bit, text.DD)
		assert.Equal(t, enums.DataTypeRate12, sent[1].Rate)
		assert.Equal(t, "N2CALL-7: Seven bits", body)
		assert.Equal(t, "N4CALL-7>"+aprs.ToCall+",TCPIP*::N2CALL-7 :ack4", r.lines(2)[1])

		out := r.radioSent(userID, n1Hotspot, &sms.Text{Format: sms.FormatTMS, Source: userID, Destination: service, Body: []byte("n2call-7 Hello there")}, enums.DataTypeRate34, "n2call-7 Hello there")
		plugins.PublishText(r.ps, out)
		assert.Equal(t, "N1CALL-9>"+aprs.ToCall+",TCPIP*::N2CALL-7 :Hello there{1", r.lines(3)[2])
		r.srv.Push("N2CALL-7>APDR16,TCPIP*,qAC,T2TEST::N1CALL-9 :ack1")

		local := r.radioSent(n4call, n4Hotspot, &sms.Text{Format: sms.FormatDefinedData, DD: enums.DD7Bit, Source: n4call, Destination: service, Body: []byte("N1CALL-9 Local")}, enums.DataTypeRate12, "N1CALL-9 Local")
		plugins.PublishText(r.ps, local)
		sent = r.texts(3)
		_, body = decoded(t, sent[2])
		assert.Equal(t, "N4CALL-7: Local", body, "a message between two users of the hub comes back from the login")
		assert.Equal(t, "N4CALL-7>"+aprs.ToCall+",TCPIP*::N1CALL-9 :Local{2", r.lines(4)[3])
		assert.Equal(t, "N1CALL-9>"+aprs.ToCall+",TCPIP*::N4CALL-7 :ack2", r.lines(5)[4])
	})
}

// TestADisabledPluginLeavesAPRSAlone turns the plugin off: the login asks
// APRS-IS for no messages, and Settings shows no switch.
func TestADisabledPluginLeavesAPRSAlone(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, be testutils.Backend) {
		r := newRig(t, be)
		r.turnOn(userID, "9", "N1CALL")
		_, err := models.UpdatePlugin(r.db, r.plugin.ID, r.plugin.Name, false, r.plugin.Ranges)
		require.NoError(t, err)
		r.gateway()
		assert.Equal(t, []string{"user K0CALL-10 pass " + passcode + " vers DMRHub 1.2.3"}, r.srv.Logins())
		u, err := models.FindUserByID(r.db, userID)
		require.NoError(t, err)
		shown, err := models.ListUserPluginSwitches(r.db, &u)
		require.NoError(t, err)
		assert.Empty(t, shown)
	})
}
