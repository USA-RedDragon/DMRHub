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

package locator_test

import (
	"encoding/binary"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/ip"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/ars"
	"github.com/USA-RedDragon/dmrgo/v2/mototrbo/lrrp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	radio   = 3191868
	service = 5057
)

type out struct {
	mu        sync.Mutex
	bursts    []*pluginv1.Burst
	positions []*pluginv1.Position
}

func (o *out) send(b *pluginv1.Burst) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.bursts = append(o.bursts, b)
	return nil
}

func (o *out) report(p *pluginv1.Position) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.positions = append(o.positions, p)
	return nil
}

func (o *out) sent() []*pluginv1.Burst {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]*pluginv1.Burst(nil), o.bursts...)
}

func (o *out) reported() []*pluginv1.Position {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]*pluginv1.Position(nil), o.positions...)
}

func newLocator(t *testing.T) (*locator.Locator, *out) {
	t.Helper()
	var o out
	l := locator.New(o.send, o.report, 5050)
	t.Cleanup(l.Stop)
	return l, &o
}

func tracking(on bool, interval uint32) *pluginv1.Tracking {
	return &pluginv1.Tracking{Radio: radio, On: on, IntervalSeconds: interval}
}

func track(l *locator.Locator, interval uint32) {
	l.SetTracked([]*pluginv1.Tracking{tracking(true, interval)})
}

func datagram(t *testing.T, port ip.Port, payload []byte) []byte {
	t.Helper()
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, radio),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, service),
		SourcePort:      port,
		DestinationPort: port,
		TTL:             64,
		Payload:         payload,
	}
	b, err := ip.AppendIPv4(nil, &d)
	require.NoError(t, err)
	return b
}

func report(t *testing.T, lat, lon float64, requestID uint32) []byte {
	t.Helper()
	m := lrrp.Message{Document: lrrp.DocumentTriggeredReport}
	var rid [4]byte
	binary.BigEndian.PutUint32(rid[:], requestID)
	m.Report.RequestID, m.Report.HasRequestID = rid[:], true
	m.Report.Circle2D = lrrp.Circle2D{Latitude: lat, Longitude: lon, Radius: 10}
	m.Report.HasCircle2D = true
	payload, err := m.AppendBinary(nil)
	require.NoError(t, err)
	return datagram(t, ip.PortLRRP, payload)
}

func registration(t *testing.T, typ ars.Type) []byte {
	t.Helper()
	m := ars.NewRegistration(ars.EventInitial)
	m.Header.Type = typ
	buf, err := m.SetDeviceID(nil, radio)
	require.NoError(t, err)
	n := len(buf)
	buf, err = m.AppendBinary(buf)
	require.NoError(t, err)
	return datagram(t, ip.PortARS, buf[n:])
}

// radioBursts returns the bursts of a data message from the radio to the
// service on slot 2.
func radioBursts(t *testing.T, payload []byte, confirmed bool) []*pluginv1.Burst {
	t.Helper()
	var h pdu.DataHeader
	var err error
	if confirmed {
		err = h.SetConfirmed(pdu.ConfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: service, LLIDSource: radio, FullMessageFlag: true})
	} else {
		err = h.SetUnconfirmed(pdu.UnconfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: service, LLIDSource: radio, FullMessageFlag: true})
	}
	require.NoError(t, err)
	tx, err := data.NewSender(&data.SenderConfig{ColorCode: 3, Source: layer2.SyncSourceMS, Slot: enums.Slot2, ResponseWait: time.Second})
	require.NoError(t, err)
	require.NoError(t, tx.Send(0, &data.Message{Header: h, Rate: enums.DataTypeRate12, Payload: payload}))
	stream := rand.Uint32()
	var outs []data.Output
	var bursts []*pluginv1.Burst
	for now := time.Duration(0); now < 3*time.Second; now += 60 * time.Millisecond {
		outs = tx.Tick(now, outs[:0])
		for _, o := range outs {
			if o.Kind != data.OutputBurst {
				continue
			}
			bursts = append(bursts, &pluginv1.Burst{
				Source: radio, Destination: service, Slot: 2, StreamId: stream, Sequence: uint32(len(bursts)), //nolint:gosec // a few bursts
				FrameType: pluginv1.FrameType_FRAME_TYPE_DATA_SYNC, DataTypeOrVoiceSequence: uint32(o.Hint.DataType),
				Payload: append([]byte(nil), o.Burst[:]...), Repeater: 319186801,
			})
		}
		if tx.State() == data.SenderIdle && len(bursts) > 0 {
			break
		}
	}
	require.NotEmpty(t, bursts)
	return bursts
}

func handle(l *locator.Locator, bursts []*pluginv1.Burst) {
	for _, b := range bursts {
		l.Handle(b)
	}
}

type answer struct {
	port ip.Port
	ars  *ars.Message
	lrrp *lrrp.Message
}

func answers(t *testing.T, bursts []*pluginv1.Burst) []answer {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: radio, Lost: -1})
	require.NoError(t, err)
	var got []answer
	var events []data.Event
	for i, b := range bursts {
		require.Equal(t, uint32(radio), b.GetDestination())
		require.False(t, b.GetGroupCall())
		raw := layer2.RawBurst(b.GetPayload())
		events = rx.Burst(time.Duration(i)*60*time.Millisecond, &raw, layer2.Hint{Kind: layer2.KindData}, events[:0])
		for _, e := range events {
			if e.Kind != data.EventDelivered && e.Kind != data.EventReassembled {
				continue
			}
			sap, ok := e.Message.Header.SAP()
			require.True(t, ok)
			d, err := ip.Decode(sap, e.Message.Payload)
			require.NoError(t, err)
			a := answer{port: d.DestinationPort}
			switch d.DestinationPort { //nolint:exhaustive
			case ip.PortARS:
				m, err := ars.DecodeMessage(d.Payload)
				require.NoError(t, err)
				a.ars = &m
			case ip.PortLRRP:
				var m lrrp.Message
				require.NoError(t, lrrp.DecodeMessageInto(&m, d.Payload))
				a.lrrp = &m
			}
			got = append(got, a)
		}
	}
	return got
}

func waitAnswers(t *testing.T, o *out, n int) []answer {
	t.Helper()
	require.Eventually(t, func() bool { return len(answers(t, o.sent())) >= n }, 5*time.Second, 50*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	return answers(t, o.sent())
}

func TestALocationReportOfATrackedRadioIsReported(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	track(l, 600)
	handle(l, radioBursts(t, report(t, 30.2672, -97.7431, 7), false))
	require.Eventually(t, func() bool { return len(o.reported()) == 1 }, 3*time.Second, 20*time.Millisecond)
	p := o.reported()[0]
	assert.Equal(t, uint32(radio), p.GetRadio())
	assert.InDelta(t, 30.2672, p.GetLatitude(), 1e-4)
	assert.InDelta(t, -97.7431, p.GetLongitude(), 1e-4)
	assert.Equal(t, pluginv1.PositionFormat_POSITION_FORMAT_LRRP, p.GetFormat())
	assert.Equal(t, uint32(2), p.GetSlot())
	assert.Equal(t, uint32(service), p.GetService())
	assert.InDelta(t, 10, p.GetAccuracyMeters(), 0)
}

func TestAReportOfAnUntrackedRadioIsNotReportedAndIsStopped(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	handle(l, radioBursts(t, report(t, 30.2672, -97.7431, 7), false))
	got := waitAnswers(t, o, 1)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].lrrp)
	assert.Equal(t, lrrp.DocumentStopRequest, got[0].lrrp.Document)
	assert.Equal(t, []byte{0, 0, 0, 7}, got[0].lrrp.Request.RequestID, "the request that the report answers")
	assert.Empty(t, o.reported())
	sent := o.sent()
	assert.Equal(t, uint32(service), sent[0].GetSource())
	assert.Equal(t, uint32(2), sent[0].GetSlot())
	assert.Equal(t, enums.ColorCode(3), layer2.RawBurst(sent[0].GetPayload()).SlotType().ColorCode, "the color code of the radio")

	handle(l, radioBursts(t, report(t, 30.2672, -97.7431, 7), false))
	time.Sleep(500 * time.Millisecond)
	assert.Len(t, answers(t, o.sent()), 1, "one stop request each ten minutes")
}

func TestAPositionAtZeroZeroIsNotReported(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	track(l, 600)
	handle(l, radioBursts(t, report(t, 0, 0, 7), false))
	time.Sleep(500 * time.Millisecond)
	assert.Empty(t, o.reported())
}

func TestAConfirmedReportIsAnswered(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	track(l, 600)
	handle(l, radioBursts(t, report(t, 30.2672, -97.7431, 7), true))
	require.Eventually(t, func() bool { return len(o.sent()) > 0 }, 3*time.Second, 20*time.Millisecond)
	ack := o.sent()[0]
	assert.Equal(t, uint32(service), ack.GetSource())
	assert.Equal(t, uint32(radio), ack.GetDestination())
	assert.Equal(t, uint32(2), ack.GetSlot())
	var b layer2.Burst
	require.NoError(t, b.DecodeFromBytes(layer2.RawBurst(ack.GetPayload())))
	dh, ok := b.Data.(*pdu.DataHeader)
	require.True(t, ok, "the answer is a data header")
	resp, ok := dh.AsResponse()
	require.True(t, ok, "the answer is a response header")
	assert.Equal(t, uint32(radio), resp.LLIDDestination)
	assert.Equal(t, uint32(service), resp.LLIDSource)
}

func TestARSRegistrationIsAcknowledged(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	handle(l, radioBursts(t, registration(t, ars.TypeDeviceRegistration), false))
	got := waitAnswers(t, o, 1)
	require.Len(t, got, 1, "an untracked radio gets the ack only")
	require.NotNil(t, got[0].ars)
	assert.Equal(t, ars.TypeAck, got[0].ars.Header.Type)
	assert.False(t, got[0].ars.Header.AckRequired, "a successful registration")
}

// registered returns a Locator that tracks the radio every interval
// seconds and that answered its registration with a request.
func registered(t *testing.T, interval uint32) (*locator.Locator, *out, *lrrp.Message) {
	t.Helper()
	l, o := newLocator(t)
	track(l, interval)
	handle(l, radioBursts(t, registration(t, ars.TypeDeviceRegistration), false))
	got := waitAnswers(t, o, 2)
	require.Len(t, got, 2)
	require.NotNil(t, got[0].ars, "the ack comes first")
	require.NotNil(t, got[1].lrrp)
	return l, o, got[1].lrrp
}

func TestARSRegistrationOfATrackedRadioStartsATriggeredRequest(t *testing.T) {
	t.Parallel()
	_, o, req := registered(t, 900)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, req.Document)
	assert.True(t, req.Request.Periodic)
	assert.True(t, req.Request.HasInterval)
	assert.Equal(t, uint32(900), req.Request.Interval)
	assert.True(t, req.Request.HasRequestID)
	last := o.sent()[len(o.sent())-1]
	assert.Equal(t, uint32(service), last.GetSource(), "from the ID the radio registered with")
}

// TestARSRegistrationBeforeTheTrackedListStartsARequest registers a radio
// before the Locator has the list of tracked radios, as when the plugin
// has just connected. The list then makes the radio tracked, and the
// registration must start the triggered request at once.
func TestARSRegistrationBeforeTheTrackedListStartsARequest(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	handle(l, radioBursts(t, registration(t, ars.TypeDeviceRegistration), false))
	require.Len(t, waitAnswers(t, o, 1), 1, "the ack")
	track(l, 900)
	got := waitAnswers(t, o, 2)
	require.Len(t, got, 2, "the tracked list did not start the request")
	require.NotNil(t, got[1].lrrp)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[1].lrrp.Document)
	assert.Equal(t, uint32(900), got[1].lrrp.Request.Interval)
	last := o.sent()[len(o.sent())-1]
	assert.Equal(t, uint32(service), last.GetSource(), "from the ID the radio registered with")
}

func TestARSDeregistrationStopsTheTriggeredRequest(t *testing.T) {
	t.Parallel()
	l, o, req := registered(t, 600)
	bursts := radioBursts(t, registration(t, ars.TypeDeviceDeregistration), false)
	handle(l, bursts)
	got := waitAnswers(t, o, 3)
	require.Len(t, got, 3)
	require.NotNil(t, got[2].lrrp)
	assert.Equal(t, lrrp.DocumentStopRequest, got[2].lrrp.Document)
	assert.Equal(t, req.Request.RequestID, got[2].lrrp.Request.RequestID)
}

func TestTurningTrackingOffSendsAStopWhereTheRequestWent(t *testing.T) {
	t.Parallel()
	l, o, req := registered(t, 600)
	l.SetTracking(tracking(false, 600))
	got := waitAnswers(t, o, 3)
	require.Len(t, got, 3)
	require.NotNil(t, got[2].lrrp)
	assert.Equal(t, lrrp.DocumentStopRequest, got[2].lrrp.Document)
	assert.Equal(t, req.Request.RequestID, got[2].lrrp.Request.RequestID)
	last := o.sent()[len(o.sent())-1]
	assert.Equal(t, uint32(service), last.GetSource())
	assert.Equal(t, uint32(2), last.GetSlot())
}

func TestARadioMissingFromTheTrackedListIsStopped(t *testing.T) {
	t.Parallel()
	l, o, _ := registered(t, 600)
	l.SetTracked(nil)
	got := waitAnswers(t, o, 3)
	require.Len(t, got, 3)
	assert.Equal(t, lrrp.DocumentStopRequest, got[2].lrrp.Document)
}

func TestANewIntervalSendsAStopAndARequestWithIt(t *testing.T) {
	t.Parallel()
	l, o, req := registered(t, 600)
	l.SetTracking(tracking(true, 1800))
	got := waitAnswers(t, o, 4)
	require.Len(t, got, 4)
	require.NotNil(t, got[2].lrrp)
	assert.Equal(t, lrrp.DocumentStopRequest, got[2].lrrp.Document)
	require.NotNil(t, got[3].lrrp)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[3].lrrp.Document)
	assert.Equal(t, uint32(1800), got[3].lrrp.Request.Interval)
	assert.NotEqual(t, req.Request.RequestID, got[3].lrrp.Request.RequestID)
}

func TestATrackingChangeOfARadioWithNoOpenRequestSendsNothing(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	track(l, 600)
	l.SetTracking(tracking(true, 1800))
	l.SetTracking(tracking(false, 1800))
	time.Sleep(300 * time.Millisecond)
	assert.Empty(t, o.sent())
}

func heardOften(l *locator.Locator, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(200 * time.Millisecond):
			l.Heard(&pluginv1.Heard{Radio: radio, Slot: 2, ColorCode: 3})
		}
	}
}

func TestASilentTrackedRadioThatIsHeardIsAskedAgain(t *testing.T) {
	t.Parallel()
	l, o, _ := registered(t, 1)
	stop := make(chan struct{})
	defer close(stop)
	go heardOften(l, stop)
	got := waitAnswers(t, o, 4)
	require.GreaterOrEqual(t, len(got), 4)
	assert.Equal(t, lrrp.DocumentStopRequest, got[2].lrrp.Document, "the silent request is stopped")
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[3].lrrp.Document)
}

func TestASilentRadioThatIsNotHeardIsNotAskedAgain(t *testing.T) {
	t.Parallel()
	_, o, _ := registered(t, 1)
	time.Sleep(5 * time.Second)
	assert.Len(t, answers(t, o.sent()), 2)
}

func TestAHeardTrackedRadioWithNoRequestIsAskedOnce(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	track(l, 600)
	stop := make(chan struct{})
	defer close(stop)
	go heardOften(l, stop)
	got := waitAnswers(t, o, 1)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].lrrp)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[0].lrrp.Document)
	assert.Equal(t, uint32(600), got[0].lrrp.Request.Interval)
	first := o.sent()[0]
	assert.Equal(t, uint32(5050), first.GetSource(), "from the plugin's first ID")
	assert.Equal(t, uint32(2), first.GetSlot())
	assert.Equal(t, enums.ColorCode(3), layer2.RawBurst(first.GetPayload()).SlotType().ColorCode)
	time.Sleep(2 * time.Second)
	assert.Len(t, answers(t, o.sent()), 1)
}

func TestARestartedLocatorLearnsTheOpenRequestFromAReport(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	track(l, 600)
	handle(l, radioBursts(t, report(t, 30.2672, -97.7431, 0xABCD), false))
	require.Eventually(t, func() bool { return len(o.reported()) == 1 }, 3*time.Second, 20*time.Millisecond)
	l.SetTracking(tracking(false, 600))
	got := waitAnswers(t, o, 1)
	require.Len(t, got, 1)
	assert.Equal(t, lrrp.DocumentStopRequest, got[0].lrrp.Document)
	assert.Equal(t, []byte{0, 0, 0xAB, 0xCD}, got[0].lrrp.Request.RequestID)
}

func TestTwoLocatorsUseDifferentRequestIDs(t *testing.T) {
	t.Parallel()
	_, _, one := registered(t, 600)
	_, _, two := registered(t, 600)
	assert.NotEqual(t, one.Request.RequestID, two.Request.RequestID)
}

func TestARegisteringRadioThatIsHeardGetsOneRequest(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	track(l, 600)
	bursts := radioBursts(t, registration(t, ars.TypeDeviceRegistration), false)
	l.Handle(bursts[0])
	l.Heard(&pluginv1.Heard{Radio: radio, Slot: 2, ColorCode: 3})
	time.Sleep(1500 * time.Millisecond)
	handle(l, bursts[1:])
	waitAnswers(t, o, 2)
	time.Sleep(time.Second)
	got := answers(t, o.sent())
	require.Len(t, got, 2, "the ack and one request")
	assert.NotNil(t, got[0].ars)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[1].lrrp.Document)
}

func TestARadioIsAskedFromTheIDItRegisteredWith(t *testing.T) {
	t.Parallel()
	l, o := newLocator(t)
	handle(l, radioBursts(t, registration(t, ars.TypeDeviceRegistration), false))
	waitAnswers(t, o, 1)
	track(l, 600)
	stop := make(chan struct{})
	defer close(stop)
	go heardOften(l, stop)
	require.Eventually(t, func() bool { return len(answers(t, o.sent())) >= 2 }, 15*time.Second, 50*time.Millisecond,
		"the request comes after the 5 seconds the Locator leaves a radio that sent data alone")
	got := answers(t, o.sent())
	require.Len(t, got, 2, "the ack, then the request once the radio is tracked")
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[1].lrrp.Document)
	last := o.sent()[len(o.sent())-1]
	assert.Equal(t, uint32(service), last.GetSource(), "from the ID the radio registered with, not the plugin's first")
}
