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

package hub_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/ars"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func confirmedReport(t *testing.T, src, dst, repeater uint) []models.Packet {
	t.Helper()
	m := lrrp.Message{Document: lrrp.DocumentUnsolicitedReport}
	m.Report.Circle2D = lrrp.Circle2D{Latitude: 30.2672, Longitude: -97.7431, Radius: 10}
	m.Report.HasCircle2D = true
	payload, err := m.AppendBinary(nil)
	require.NoError(t, err)
	return dataCall(t, src, dst, repeater, ip.PortLRRP, payload, true)
}

func arsRegistration(t *testing.T, src, dst, repeater uint) []models.Packet {
	t.Helper()
	m := ars.NewRegistration(ars.EventInitial)
	buf, err := m.SetDeviceID(nil, uint32(src))
	require.NoError(t, err)
	n := len(buf)
	buf, err = m.AppendBinary(buf)
	require.NoError(t, err)
	return dataCall(t, src, dst, repeater, ip.PortARS, buf[n:], false)
}

func dataCall(t *testing.T, src, dst, repeater uint, port ip.Port, payload []byte, confirmed bool) []models.Packet {
	t.Helper()
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, uint32(src)),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, uint32(dst)),
		SourcePort:      port,
		DestinationPort: port,
		TTL:             64,
		Payload:         payload,
	}
	packet, err := ip.AppendIPv4(nil, &d)
	require.NoError(t, err)
	var h pdu.DataHeader
	if confirmed {
		require.NoError(t, h.SetConfirmed(pdu.ConfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: uint32(dst), LLIDSource: uint32(src), FullMessageFlag: true}))
	} else {
		require.NoError(t, h.SetUnconfirmed(pdu.UnconfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: uint32(dst), LLIDSource: uint32(src), FullMessageFlag: true}))
	}
	tx, err := data.NewSender(&data.SenderConfig{ColorCode: 1, Source: layer2.SyncSourceMS, Slot: enums.Slot2, ResponseWait: time.Second})
	require.NoError(t, err)
	require.NoError(t, tx.Send(0, &data.Message{Header: h, Rate: enums.DataTypeRate12, Payload: packet}))
	var outs []data.Output
	var pkts []models.Packet
	for now := time.Duration(0); now < 3*time.Second && tx.State() != data.SenderWaitResponse && (confirmed || tx.State() != data.SenderIdle || len(pkts) == 0); now += 60 * time.Millisecond {
		outs = tx.Tick(now, outs[:0])
		for _, o := range outs {
			if o.Kind == data.OutputBurst {
				pkts = append(pkts, models.Packet{
					Signature: string(dmrconst.CommandDMRD), Seq: uint(len(pkts)), Src: src, Dst: dst,
					Repeater: repeater, Slot: true, FrameType: dmrconst.FrameDataSync,
					DTypeOrVSeq: uint(o.Hint.DataType), StreamID: 0xD47B, DMRData: o.Burst, BER: -1, RSSI: -1,
				})
			}
		}
	}
	require.NotEmpty(t, pkts)
	return pkts
}

// locatorHub returns lastSeenHub with the location plugin at 5050 to
// 5059.
func locatorHub(t *testing.T) (*hub.Hub, *gorm.DB, *hub.ServerHandle) {
	t.Helper()
	h, database, ps, srv := lastSeenHubWithPubSub(t)
	listener := testutils.StartPlugins(t, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) }, nil)
	listener.Run(t, "Location", []models.PluginRange{{First: 5050, Last: 5059}}, locator.Serve)
	return h, database, srv
}

const answerQuiet = 500 * time.Millisecond

func nextFrom(srv *hub.ServerHandle, d time.Duration) (hub.RoutedPacket, bool) {
	rp, got, _ := nextOr(srv, nil, d)
	return rp, got
}

func nextOr(srv *hub.ServerHandle, changed <-chan struct{}, d time.Duration) (rp hub.RoutedPacket, got, ok bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case rp := <-srv.Packets:
		return rp, true, true
	case <-changed:
		return hub.RoutedPacket{}, false, true
	case <-timer.C:
		return hub.RoutedPacket{}, false, false
	}
}

func TestDataToTheLocationServerIsAnsweredOnItsRepeater(t *testing.T) {
	t.Parallel()
	h, _, srv := locatorHub(t)

	for _, p := range confirmedReport(t, 1000002, 5057, 100003) {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	rp, ok := nextFrom(srv, failAfter)
	require.True(t, ok, "the location server did not answer a confirmed report")
	assert.Equal(t, uint(100003), rp.RepeaterID, "the answer goes out where the report came in")
	assert.Equal(t, uint(5057), rp.Packet.Src)
	assert.Equal(t, uint(1000002), rp.Packet.Dst)
	assert.False(t, rp.Packet.GroupCall)
	if rp, ok := nextFrom(srv, answerQuiet); ok {
		t.Fatalf("only the answer goes out, but a packet went to %d", rp.RepeaterID)
	}
}

type locatorClock struct {
	*clock.Fake
	mu      sync.Mutex
	armed   map[*locatorTimer]time.Time
	changed chan struct{}
}

type locatorTimer struct {
	c     *locatorClock
	inner clock.Timer
}

func newLocatorClock() *locatorClock {
	return &locatorClock{Fake: clock.NewFake(time.Now()), armed: map[*locatorTimer]time.Time{}, changed: make(chan struct{}, 1)}
}

func (c *locatorClock) notify() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

func (c *locatorClock) AfterFunc(d time.Duration, f func()) clock.Timer {
	t := &locatorTimer{c: c}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.inner = c.Fake.AfterFunc(d, func() {
		c.mu.Lock()
		delete(c.armed, t)
		c.mu.Unlock()
		f()
	})
	t.note(d)
	return t
}

func (t *locatorTimer) note(d time.Duration) {
	if d > 0 {
		t.c.armed[t] = t.c.Now().Add(d)
	} else {
		delete(t.c.armed, t)
	}
	t.c.notify()
}

func (t *locatorTimer) C() <-chan time.Time { return t.inner.C() }

func (t *locatorTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	t.note(0)
	return t.inner.Stop()
}

func (t *locatorTimer) Reset(d time.Duration) bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := t.inner.Reset(d)
	t.note(d)
	return was
}

func (c *locatorClock) next() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var at time.Time
	ok := false
	for _, t := range c.armed {
		if !ok || t.Before(at) {
			at, ok = t, true
		}
	}
	return at, ok
}

const locatorStep = time.Second

func (c *locatorClock) drive(t *testing.T, srv *hub.ServerHandle, take func(hub.RoutedPacket) bool) {
	t.Helper()
	for {
		if at, ok := c.next(); ok && !at.After(c.Now().Add(locatorStep)) {
			c.Advance(c.Until(at))
		}
		rp, got, ok := nextOr(srv, c.changed, failAfter)
		require.True(t, ok, "the Locator did not send all that the test waits for")
		if got && take(rp) {
			return
		}
	}
}

type radioMessages struct {
	rx     *data.Receiver
	n      int
	events []data.Event
	got    []answerOf
}

type answerOf struct {
	port     ip.Port
	ars      bool
	document lrrp.DocumentID
}

func newRadioMessages(t *testing.T) *radioMessages {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: 1000002, Lost: -1})
	require.NoError(t, err)
	return &radioMessages{rx: rx}
}

func (m *radioMessages) take(t *testing.T, rp hub.RoutedPacket) {
	t.Helper()
	require.Equal(t, uint(100003), rp.RepeaterID, "the Locator's data goes where the radio is heard")
	require.Equal(t, uint(1000002), rp.Packet.Dst)
	raw := layer2.RawBurst(rp.Packet.DMRData)
	m.events = m.rx.Burst(time.Duration(m.n)*60*time.Millisecond, &raw, layer2.Hint{Kind: layer2.KindData}, m.events[:0])
	m.n++
	for _, e := range m.events {
		if e.Kind != data.EventDelivered && e.Kind != data.EventReassembled {
			continue
		}
		sap, ok := e.Message.Header.SAP()
		require.True(t, ok)
		d, err := ip.Decode(sap, e.Message.Payload)
		require.NoError(t, err)
		a := answerOf{port: d.DestinationPort}
		switch d.DestinationPort { //nolint:exhaustive
		case ip.PortARS:
			_, err := ars.DecodeMessage(d.Payload)
			require.NoError(t, err)
			a.ars = true
		case ip.PortLRRP:
			var l lrrp.Message
			require.NoError(t, lrrp.DecodeMessageInto(&l, d.Payload))
			a.document = l.Document
		}
		m.got = append(m.got, a)
	}
}

func nextHeard(t *testing.T, sub pubsub.Subscription) (repeater uint, at time.Time) {
	t.Helper()
	for {
		b := bubble.Receive(t, sub.Channel(), failAfter, "the hub did not tell the plugins where the radio was heard")
		radio, repeater, _, _, at, ok := plugins.DecodeHeard(plugins.HeardAtTopic, b)
		require.True(t, ok)
		if radio == 1000002 {
			return repeater, at
		}
	}
}

// TestASilentTrackedRadioIsAskedAgainWhereItIsHeard moves the time only
// while nothing is on its way to the Locator that a timer could cut short.
// The Locator sends the first burst of its answer to the registration
// without a timer, once it has all of the registration. The radio
// registers before its owner turns tracking on, so that no ask of a heard
// radio is due before then.
func TestASilentTrackedRadioIsAskedAgainWhereItIsHeard(t *testing.T) {
	t.Parallel()
	c := newLocatorClock()
	h, database, ps, srv := lastSeenHubWrappingKV(t, keepKV, c.Fake)
	heard := ps.Subscribe(plugins.HeardAtTopic)
	t.Cleanup(func() { _ = heard.Close() })
	listener := testutils.StartPlugins(t, database, ps, h, func(r *plugins.Registry) { h.SetPlugins(r) }, nil)
	listener.Run(t, "Location", []models.PluginRange{{First: 5050, Last: 5059}}, func(ctx context.Context, conn *plugin.Conn) error {
		return locator.Serve(clock.NewContext(ctx, c), conn)
	})

	registeredAt := c.Now()
	for _, p := range arsRegistration(t, 1000002, 5057, 100003) {
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	repeater, at := nextHeard(t, heard)
	assert.Equal(t, uint(100003), repeater)
	assert.Equal(t, registeredAt.UnixMilli(), at.UnixMilli(), "the time the hub heard the registration")
	msgs := newRadioMessages(t)
	msgs.take(t, bubble.Receive(t, srv.Packets, failAfter, "the Locator did not answer the registration"))
	c.drive(t, srv, func(rp hub.RoutedPacket) bool {
		msgs.take(t, rp)
		return len(msgs.got) >= 1
	})
	require.NoError(t, models.SetPositionTracking(database, 1000002, 1000002, true, time.Now()))
	require.NoError(t, database.Model(&models.PositionSetting{}).Where("radio_id = ?", 1000002).Update("interval_seconds", 1).Error)
	require.NoError(t, plugins.PublishTracking(ps, 1000002))
	c.drive(t, srv, func(rp hub.RoutedPacket) bool {
		msgs.take(t, rp)
		return len(msgs.got) >= 2
	})
	require.Equal(t, []answerOf{{port: ip.PortARS, ars: true}, {port: ip.PortLRRP, document: lrrp.DocumentTriggeredRequest}}, msgs.got,
		"the registration is acknowledged, then the radio is asked for its position once its owner turns tracking on")

	c.Advance(3*time.Second + locatorStep)
	silentAt := c.Now()
	foreign := makeVoicePacket(1, 70999, true, true)
	foreign.Src, foreign.Repeater = 1000002, 100004
	h.RoutePacket(context.Background(), foreign, models.RepeaterTypeMMDVM)
	voice := makeVoicePacket(1, 71000, true, true)
	voice.Src, voice.Repeater = 1000002, 100003
	h.RoutePacket(context.Background(), voice, models.RepeaterTypeMMDVM)
	repeater, at = nextHeard(t, heard)
	assert.Equal(t, uint(100003), repeater, "a sighting on another user's hotspot is not told to the plugins")
	assert.Equal(t, silentAt.UnixMilli(), at.UnixMilli())

	c.drive(t, srv, func(rp hub.RoutedPacket) bool {
		if rp.Packet.Src != 5057 {
			return false
		}
		msgs.take(t, rp)
		return len(msgs.got) >= 4
	})
	assert.Equal(t, []answerOf{{port: ip.PortLRRP, document: lrrp.DocumentStopRequest}, {port: ip.PortLRRP, document: lrrp.DocumentTriggeredRequest}}, msgs.got[2:],
		"the silent request is stopped and the radio asked again where it is heard")
}
