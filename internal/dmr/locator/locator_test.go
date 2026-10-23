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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/locator"
	"github.com/USA-RedDragon/configulator/v2"
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

const (
	radio    = 3191868
	service  = 5057
	repeater = 319186801
)

type sent struct {
	mu   sync.Mutex
	pkts []models.Packet
}

func (s *sent) send(p models.Packet) {
	s.mu.Lock()
	s.pkts = append(s.pkts, p)
	s.mu.Unlock()
}

func (s *sent) all() []models.Packet {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Packet(nil), s.pkts...)
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:locator_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, database.Create(&models.User{ID: radio, Callsign: "KI5VMF", Username: "a", Approved: true}).Error)
	return database
}

func report(t *testing.T, lat, lon float64) []byte {
	t.Helper()
	m := lrrp.Message{Document: lrrp.DocumentTriggeredReport}
	var rid [4]byte
	binary.BigEndian.PutUint32(rid[:], 7)
	m.Report.RequestID, m.Report.HasRequestID = rid[:], true
	m.Report.Circle2D = lrrp.Circle2D{Latitude: lat, Longitude: lon, Radius: 10}
	m.Report.HasCircle2D = true
	payload, err := m.AppendBinary(nil)
	require.NoError(t, err)
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, radio),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, service),
		SourcePort:      ip.PortLRRP,
		DestinationPort: ip.PortLRRP,
		TTL:             64,
		Payload:         payload,
	}
	out, err := ip.AppendIPv4(nil, &d)
	require.NoError(t, err)
	return out
}

func radioBursts(t *testing.T, payload []byte, confirmed bool) []models.Packet {
	t.Helper()
	var h pdu.DataHeader
	var err error
	if confirmed {
		err = h.SetConfirmed(pdu.ConfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: service, LLIDSource: radio, FullMessageFlag: true})
	} else {
		err = h.SetUnconfirmed(pdu.UnconfirmedDataHeader{SAP: enums.SAPIPBasedPacketData, LLIDDestination: service, LLIDSource: radio, FullMessageFlag: true})
	}
	require.NoError(t, err)
	tx, err := data.NewSender(&data.SenderConfig{ColorCode: 1, Source: layer2.SyncSourceMS, Slot: enums.Slot2, ResponseWait: time.Second})
	require.NoError(t, err)
	require.NoError(t, tx.Send(0, &data.Message{Header: h, Rate: enums.DataTypeRate12, Payload: payload}))
	var outs []data.Output
	var pkts []models.Packet
	for now := time.Duration(0); now < 3*time.Second; now += 60 * time.Millisecond {
		outs = tx.Tick(now, outs[:0])
		for _, o := range outs {
			if o.Kind != data.OutputBurst {
				continue
			}
			pkts = append(pkts, models.Packet{
				Signature: string(dmrconst.CommandDMRD), Seq: uint(len(pkts)), Src: radio, Dst: service,
				Repeater: repeater, Slot: true, FrameType: dmrconst.FrameDataSync,
				DTypeOrVSeq: uint(o.Hint.DataType), StreamID: 0xD47A, DMRData: o.Burst, BER: -1, RSSI: -1,
			})
		}
		if tx.State() == data.SenderIdle && len(pkts) > 0 {
			break
		}
	}
	require.NotEmpty(t, pkts)
	return pkts
}

func positions(t *testing.T, database *gorm.DB) []models.Position {
	t.Helper()
	var out []models.Position
	require.NoError(t, database.Order("id").Find(&out).Error)
	return out
}

func TestALocationReportOfATrackedRadioIsStored(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	require.NoError(t, models.SetPositionTracking(database, radio, radio, true, time.Now().Add(-time.Minute)))
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, report(t, 30.2672, -97.7431), false) {
		l.Handle(p)
	}
	require.Eventually(t, func() bool { return len(positions(t, database)) == 1 }, 3*time.Second, 20*time.Millisecond)
	p := positions(t, database)[0]
	assert.Equal(t, uint(radio), p.RadioID)
	assert.InDelta(t, 30.2672, p.Latitude, 1e-4)
	assert.InDelta(t, -97.7431, p.Longitude, 1e-4)
	assert.Equal(t, models.PositionFormatLRRP, p.Format)
	assert.Equal(t, uint(repeater), p.RepeaterID)
	assert.Equal(t, uint8(2), p.Slot)
	assert.Equal(t, uint(service), p.ServiceID)
}

func TestAReportOfAnUntrackedRadioIsNotStored(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, report(t, 30.2672, -97.7431), false) {
		l.Handle(p)
	}
	time.Sleep(500 * time.Millisecond)
	assert.Empty(t, positions(t, database))
}

func TestAPositionAtZeroZeroIsNotStored(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	require.NoError(t, models.SetPositionTracking(database, radio, radio, true, time.Now().Add(-time.Minute)))
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, report(t, 0, 0), false) {
		l.Handle(p)
	}
	time.Sleep(500 * time.Millisecond)
	assert.Empty(t, positions(t, database))
}

func TestAConfirmedReportIsAnswered(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, report(t, 30.2672, -97.7431), true) {
		l.Handle(p)
	}
	require.Eventually(t, func() bool { return len(out.all()) > 0 }, 3*time.Second, 20*time.Millisecond)
	ack := out.all()[0]
	assert.Equal(t, uint(service), ack.Src)
	assert.Equal(t, uint(radio), ack.Dst)
	assert.False(t, ack.GroupCall)
	assert.Equal(t, uint(repeater), ack.Repeater)
	assert.True(t, ack.Slot)
	var b layer2.Burst
	require.NoError(t, b.DecodeFromBytes(layer2.RawBurst(ack.DMRData)))
	dh, ok := b.Data.(*pdu.DataHeader)
	require.True(t, ok, "the answer is a data header")
	resp, ok := dh.AsResponse()
	require.True(t, ok, "the answer is a response header")
	assert.Equal(t, uint32(radio), resp.LLIDDestination)
	assert.Equal(t, uint32(service), resp.LLIDSource)
}

func TestTrafficToOtherIDsIsNotTheLocators(t *testing.T) {
	t.Parallel()
	for _, id := range []uint{5049, 5060, 9990, 3191868} {
		assert.False(t, locator.IsService(id), "%d", id)
	}
	for id := uint(5050); id <= 5059; id++ {
		assert.True(t, locator.IsService(id), "%d", id)
	}
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
	d := ip.Datagram{
		Source:          ip.RadioAddress(ip.DefaultNetwork, radio),
		Destination:     ip.RadioAddress(ip.DefaultNetwork, service),
		SourcePort:      ip.PortARS,
		DestinationPort: ip.PortARS,
		TTL:             64,
		Payload:         buf[n:],
	}
	out, err := ip.AppendIPv4(nil, &d)
	require.NoError(t, err)
	return out
}

type answer struct {
	port ip.Port
	ars  *ars.Message
	lrrp *lrrp.Message
}

func answers(t *testing.T, pkts []models.Packet) []answer {
	t.Helper()
	rx, err := data.NewReceiver(&data.ReceiverConfig{Address: radio, Lost: -1})
	require.NoError(t, err)
	var out []answer
	var events []data.Event
	for i, p := range pkts {
		require.Equal(t, uint(service), p.Src)
		require.Equal(t, uint(radio), p.Dst)
		events = rx.Burst(time.Duration(i)*60*time.Millisecond, (*layer2.RawBurst)(&p.DMRData), layer2.Hint{Kind: layer2.KindData}, events[:0])
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
			out = append(out, a)
		}
	}
	return out
}

func waitAnswers(t *testing.T, out *sent, n int) []answer {
	t.Helper()
	var got []answer
	require.Eventually(t, func() bool {
		got = answers(t, out.all())
		return len(got) >= n
	}, 5*time.Second, 50*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	return answers(t, out.all())
}

func TestARSRegistrationIsAcknowledged(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, registration(t, ars.TypeDeviceRegistration), false) {
		l.Handle(p)
	}
	got := waitAnswers(t, &out, 1)
	require.Len(t, got, 1, "an untracked radio gets the ack only")
	require.NotNil(t, got[0].ars)
	assert.Equal(t, ars.TypeAck, got[0].ars.Header.Type)
	assert.False(t, got[0].ars.Header.AckRequired, "a successful registration")
}

func TestARSRegistrationOfATrackedRadioStartsATriggeredRequest(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	require.NoError(t, models.SetPositionTracking(database, radio, radio, true, time.Now()))
	require.NoError(t, database.Model(&models.PositionSetting{}).Where("radio_id = ?", radio).Update("interval_seconds", 900).Error)
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, registration(t, ars.TypeDeviceRegistration), false) {
		l.Handle(p)
	}
	got := waitAnswers(t, &out, 2)
	require.Len(t, got, 2)
	require.NotNil(t, got[0].ars, "the ack comes first")
	req := got[1].lrrp
	require.NotNil(t, req)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, req.Document)
	assert.True(t, req.Request.Periodic)
	assert.True(t, req.Request.HasInterval)
	assert.Equal(t, uint32(900), req.Request.Interval)
	assert.True(t, req.Request.HasRequestID)

	var row models.LocationRequest
	require.NoError(t, database.First(&row, radio).Error)
	assert.Equal(t, uint(service), row.ServiceID)
	assert.Equal(t, uint(900), row.IntervalSeconds)
	assert.Equal(t, req.Request.RequestID, row.RequestID)
	assert.Equal(t, uint(repeater), row.RepeaterID)
	assert.Equal(t, uint8(2), row.Slot)
}

func TestARSDeregistrationStopsTheTriggeredRequest(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	require.NoError(t, models.SetPositionTracking(database, radio, radio, true, time.Now()))
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, registration(t, ars.TypeDeviceRegistration), false) {
		l.Handle(p)
	}
	waitAnswers(t, &out, 2)
	for _, p := range radioBursts(t, registration(t, ars.TypeDeviceDeregistration), false) {
		p.StreamID++
		l.Handle(p)
	}
	got := waitAnswers(t, &out, 3)
	require.Len(t, got, 3)
	require.NotNil(t, got[2].lrrp)
	assert.Equal(t, lrrp.DocumentStopRequest, got[2].lrrp.Document)
	var n int64
	require.NoError(t, database.Model(&models.LocationRequest{}).Where("radio_id = ?", radio).Count(&n).Error)
	assert.Zero(t, n)
}

func TestASilentTrackedRadioThatIsHeardIsAskedAgain(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	require.NoError(t, models.SetPositionTracking(database, radio, radio, true, time.Now()))
	require.NoError(t, database.Model(&models.PositionSetting{}).Where("radio_id = ?", radio).Update("interval_seconds", 1).Error)
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, registration(t, ars.TypeDeviceRegistration), false) {
		l.Handle(p)
	}
	waitAnswers(t, &out, 2)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(200 * time.Millisecond):
				l.Heard(radio, repeater, true)
			}
		}
	}()
	got := waitAnswers(t, &out, 3)
	require.GreaterOrEqual(t, len(got), 3)
	require.NotNil(t, got[2].lrrp)
	assert.Equal(t, lrrp.DocumentTriggeredRequest, got[2].lrrp.Document)
}

func TestASilentRadioThatIsNotHeardIsNotAskedAgain(t *testing.T) {
	t.Parallel()
	database := openDB(t)
	require.NoError(t, models.SetPositionTracking(database, radio, radio, true, time.Now()))
	require.NoError(t, database.Model(&models.PositionSetting{}).Where("radio_id = ?", radio).Update("interval_seconds", 1).Error)
	var out sent
	l := locator.New(database, out.send)
	defer l.Stop()

	for _, p := range radioBursts(t, registration(t, ars.TypeDeviceRegistration), false) {
		l.Handle(p)
	}
	waitAnswers(t, &out, 2)
	time.Sleep(5 * time.Second)
	assert.Len(t, answers(t, out.all()), 2)
}
