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

package mailbox_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugintexts"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/DMRHub/plugins/mailbox"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
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
	aliceSpot = 100001
	bobSpot   = 100002
	// answerWait bounds the wait for an answer of the mailbox, which
	// comes as soon as the hub and the plugin handled the text.
	answerWait = time.Minute
)

type noRoute struct{}

type noPrompts struct{}

func (noPrompts) PlayPrompt(context.Context, uint, uint, bool, []string, uint) error { return nil }

func (noRoute) RoutePacket(context.Context, models.Packet, string) {}

type sentData struct {
	repeater uint
	slot     bool
	m        data.Message
}

// sender is the hub's side of the radio link: it takes each text that the
// hub sends, as the radio would.
type sender struct {
	got chan sentData
}

func (s *sender) SendData(_ context.Context, repeater uint, slot bool, m data.Message) (data.Result, error) {
	s.got <- sentData{repeater, slot, m}
	return data.ResultSent, nil
}

// rig is a hub with the mailbox at box, on a database of a backend.
type rig struct {
	t      *testing.T
	db     *gorm.DB
	ps     pubsub.PubSub
	srv    *plugins.Server
	cfg    *config.Config
	out    *sender
	mail   <-chan string
	token  string
	stop   func()
	nextID uint
}

func backends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()}
}

func newRig(t *testing.T, be testutils.Backend) *rig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = kvStore.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	mail := testutils.StartSMTPSink(t, &cfg)
	cfg.Secret = "a secret of the config, long enough"
	cfg.DMR.Plugins = config.Plugins{Enabled: true, Bind: "127.0.0.1", Port: 0}
	for _, u := range []models.User{
		{ID: alice, Callsign: "N1CALL", Username: "n1call", Approved: true, Email: "n1call@example.com", EmailVerified: true},
		{ID: bob, Callsign: "N2CALL", Username: "n2call", Approved: true},
	} {
		require.NoError(t, database.Create(&u).Error)
	}
	require.NoError(t, database.Create(&models.UserRadioID{UserID: alice, RadioID: aliceHT, VerifiedAt: time.Now()}).Error)
	require.NoError(t, database.Create(&models.Repeater{ID: aliceSpot, OwnerID: alice, Callsign: "N1CALL", Type: models.RepeaterTypeMMDVM}).Error)
	require.NoError(t, database.Create(&models.Repeater{ID: bobSpot, OwnerID: bob, Callsign: "N2CALL", Type: models.RepeaterTypeMMDVM}).Error)
	out := &sender{got: make(chan sentData, 16)}
	srv, err := plugins.Start(context.Background(), &cfg, database, ps, noRoute{}, func(*plugins.Registry) {}, nil,
		plugins.WithTexts(plugintexts.New(database, out)), plugins.WithPrompts(noPrompts{}),
		plugins.WithEmail(plugins.Email{Send: smtp.NewMailer(&cfg).SendText, On: func() bool { return true }, Limits: kvStore}))
	require.NoError(t, err)
	t.Cleanup(srv.Stop)
	p, token, err := models.CreatePlugin(database, "Mailbox", []models.PluginRange{{First: box, Last: box}}, time.Now())
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(ps))
	require.NoError(t, srv.Registry().Reload())
	r := &rig{t: t, db: database, ps: ps, srv: srv, cfg: &cfg, out: out, mail: mail, token: token}
	r.start()
	t.Cleanup(func() { r.stop() })
	u, err := models.SetUserPluginSwitch(database, p.ID, alice, mailbox.EmailSwitch, true, "", time.Now())
	require.NoError(t, err, "the hub kept the switch of the mailbox's hello")
	require.NoError(t, plugins.PublishSwitch(ps, p.ID, &u))
	return r
}

// start runs the mailbox, and returns once the hub sends the texts to it.
func (r *rig) start() {
	r.t.Helper()
	certs, err := plugins.NewCertSource(r.cfg.GetDerivedSecret(), "", "")
	require.NoError(r.t, err)
	fp, err := certs.Fingerprint()
	require.NoError(r.t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ready := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		plugin.Run(ctx, plugin.Options{Address: r.srv.Addr().String(), Token: r.token, Fingerprint: fp, Hello: mailbox.Hello()}, func(ctx context.Context, c *plugin.Conn) error {
			return mailbox.ServeAndTell(ctx, c, func() { once.Do(func() { close(ready) }) })
		})
	}()
	r.stop = func() {
		cancel()
		<-done
	}
	within(r.t, ready, "the subscription of the mailbox")
}

// within returns the next value of ch, and fails the test after
// answerWait, which only ends a hung test.
func within[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	timer := time.NewTimer(answerWait)
	defer timer.Stop()
	select {
	case v := <-ch:
		return v
	case <-timer.C:
		t.Fatalf("no %s within %s", what, answerWait)
	}
	var zero T
	return zero
}

// text stores a text from radio through repeater in the format and rate,
// as the hub reads one, and gives it to the plugins.
func (r *rig) text(radio, repeater uint, format sms.Format, rate enums.DataType, body string) {
	r.t.Helper()
	plugins.PublishText(r.ps, r.store(radio, repeater, format, rate, body))
}

// store stores a text from radio through repeater in the format and rate,
// as the hub reads one.
func (r *rig) store(radio, repeater uint, format sms.Format, rate enums.DataType, body string) *models.Message {
	r.t.Helper()
	r.nextID++
	text := &sms.Text{Format: format, Source: uint32(radio), Destination: box, Sequence: uint8(r.nextID % 16), Body: []byte(body)}
	modelFormat := models.MessageFormatTMS
	if format == sms.FormatDefinedData {
		text.DD = enums.DDISO8859Part1
		modelFormat = models.MessageFormatDefinedData
	}
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(r.t, err)
	p, err := packet.NewPacket(h.Data, payload, rate)
	require.NoError(r.t, err)
	head, err := p.Header.Encode()
	require.NoError(r.t, err)
	m := &models.Message{
		ReceivedAt: time.Now(), SourceID: radio, DestinationID: box, RepeaterID: repeater, Slot: 2, Format: modelFormat,
		ChecksumOK: true, Rate: uint8(rate), Raw: append(head[:], payload...), Text: &body,
	}
	require.NoError(r.t, r.db.Create(m).Error)
	return m
}

// answer returns the next text that the hub sent, decoded.
func (r *rig) answer() (sentData, *sms.Text, string) {
	r.t.Helper()
	got := within(r.t, r.out.got, "text")
	body, text, err := sms.Decode(nil, &got.m.Header, got.m.Payload)
	require.NoError(r.t, err)
	return got, &text, string(body)
}

// ask sends body from radio through repeater as TMS and returns the
// answer.
func (r *rig) ask(radio, repeater uint, body string) string {
	r.t.Helper()
	r.text(radio, repeater, sms.FormatTMS, enums.DataTypeRate34, body)
	_, _, got := r.answer()
	return got
}

func (r *rig) email() string {
	r.t.Helper()
	return within(r.t, r.mail, "email")
}

func TestTheMailboxWorksThroughTheHubOnEachDatabase(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t, be)
			assert.Equal(t, "No messages for N1CALL.", r.ask(alice, aliceSpot, "INBOX"))

			r.text(bob, bobSpot, sms.FormatDefinedData, enums.DataTypeRate12, "N1CALL Net moved to 8 PM")
			got, text, body := r.answer()
			assert.Equal(t, "Saved for N1CALL.", body)
			assert.Equal(t, sms.FormatDefinedData, text.Format, "the answer is in the format of the text")
			assert.Equal(t, enums.DataTypeRate12, got.m.Rate)
			assert.Equal(t, uint(bobSpot), got.repeater)
			assert.True(t, got.slot)
			dst, src, ok := got.m.Header.Addresses()
			require.True(t, ok)
			assert.Equal(t, []uint32{box, bob}, []uint32{src, dst})

			untrusted := r.store(bob, aliceSpot, sms.FormatDefinedData, enums.DataTypeRate12, "INBOX")
			dd := r.store(bob, bobSpot, sms.FormatDefinedData, enums.DataTypeRate12, "INBOX")
			tms := r.store(bob, bobSpot, sms.FormatTMS, enums.DataTypeRate34, "INBOX")
			for _, m := range []*models.Message{untrusted, dd, tms} {
				plugins.PublishText(r.ps, m)
			}
			got, text, body = r.answer()
			assert.Equal(t, "No messages for N2CALL.", body)
			assert.Equal(t, sms.FormatDefinedData, text.Format, "no answer to a text through a hotspot that is not the radio's owner's, and the answer is in the format of its own text, not of the radio's last one")
			assert.Equal(t, enums.DataTypeRate12, got.m.Rate)
			got, text, _ = r.answer()
			assert.Equal(t, sms.FormatTMS, text.Format)
			assert.Equal(t, enums.DataTypeRate34, got.m.Rate)

			mail := r.email()
			assert.Contains(t, mail, "To: n1call@example.com")
			assert.Contains(t, mail, "Subject: New message for N1CALL")
			assert.Contains(t, mail, "N2CALL left a message for N1CALL in the mailbox.")
			assert.NotContains(t, mail, "Net moved", "the email has no text of the message")

			require.NoError(t, plugins.PublishHeard(r.ps, aliceHT, aliceSpot, 2, 1))
			r.stop()
			r.start()
			require.NoError(t, plugins.PublishHeard(r.ps, alice, aliceSpot, 2, 1))
			got, text, body = r.answer()
			assert.Equal(t, "1 new message for N1CALL. Text INBOX to 4321.", body, "the notice lasts over a restart, and goes to a radio that texted")
			assert.Equal(t, uint(aliceSpot), got.repeater)
			assert.Equal(t, sms.FormatTMS, text.Format)

			assert.Equal(t, "1 message: 1 N2CALL Net moved to...", r.ask(alice, aliceSpot, "INBOX"), "the message lasts over a restart")
			getBody := r.ask(alice, aliceSpot, "GET 1")
			assert.True(t, strings.HasPrefix(getBody, "1/1 N2CALL "), getBody)
			assert.True(t, strings.HasSuffix(getBody, "Z: Net moved to 8 PM"), getBody)
			assert.Equal(t, "Deleted all 1 message.", r.ask(alice, aliceSpot, "DELETE ALL"))
			assert.Equal(t, "No user has callsign N0NONE on this network.", r.ask(bob, bobSpot, "N0NONE hi"))
		})
	}
}
