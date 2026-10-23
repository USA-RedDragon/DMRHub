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
