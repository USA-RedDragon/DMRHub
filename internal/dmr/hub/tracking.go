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

package hub

import (
	"math"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
)

const (
	positionsWork = "positions"
	fullGPSSize   = 18
)

// PositionNotify is told of each position that the hub stored.
type PositionNotify func(models.Position)

// RecordTalkerAlias records the talker alias block a that the repeater
// with the ID repeater sent to the server named sourceName.
func (h *Hub) RecordTalkerAlias(a dmrd.TalkerAlias, sourceName string, repeater uint) {
	h.callTracker.TalkerAlias(a, sourceName, repeater)
}

// RecordPosition records the GPS position p that the repeater with the ID
// repeater sent to the server named sourceName. It marks the call of its
// source, and stores p as a position of the radio under the rules of
// models.StorePosition, the same as a position from a data call.
func (h *Hub) RecordPosition(p dmrd.Position, sourceName string, repeater uint) {
	h.callTracker.Position(p, sourceName, repeater)
	pos, ok := embeddedPosition(p, repeater, time.Now())
	if !ok || h.db == nil {
		return
	}
	h.later.Add(laterKey{what: positionsWork, id: pos.RadioID}, outbox.Job{
		Topic:      positionsWork,
		Repeatable: true,
		Send:       func() error { return h.storePosition(pos) },
	})
}

// SetPositionNotify sets the function that is told of each position that
// RecordPosition stored.
func (h *Hub) SetPositionNotify(f PositionNotify) {
	h.positionNotify.Store(&f)
}

func (h *Hub) storePosition(pos models.Position) error {
	stored, err := models.StorePosition(h.db, pos)
	if err != nil {
		return err //nolint:wrapcheck // StorePosition wraps its errors
	}
	if f := h.positionNotify.Load(); stored && f != nil && *f != nil {
		(*f)(pos)
	}
	return nil
}

// embeddedPosition returns the position of the GPS Info LC in p, heard on
// repeater at now, and false when p is the short form without
// coordinates or does not hold a place on Earth.
func embeddedPosition(p dmrd.Position, repeater uint, now time.Time) (models.Position, bool) {
	if p.Size != fullGPSSize || p.Source == 0 {
		return models.Position{}, false
	}
	lc, err := p.LC()
	if err != nil {
		return models.Position{}, false
	}
	gps, ok := lc.AsGPSInfo()
	if !ok {
		return models.Position{}, false
	}
	lat, lon := gps.Latitude, gps.Longitude
	if math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 || (lat == 0 && lon == 0) {
		return models.Position{}, false
	}
	return models.Position{
		RadioID: uint(p.Source), Latitude: lat, Longitude: lon, Format: models.PositionFormatGPS,
		AccuracyM: positionAccuracy(gps), ReceivedAt: now, RepeaterID: repeater, Slot: 1,
	}, true
}

// positionAccuracy returns the upper bound of the Position_Error of gps in
// meters (ETSI TS 102 361-2 clause 7.2.15), and nil when it is not known
// or has no bound.
func positionAccuracy(gps pdu.FLCGPSInfo) *float64 {
	var m float64
	switch gps.PositionError {
	case layer3.PositionLessThan2M:
		m = 2
	case layer3.PositionLessThan20M:
		m = 20
	case layer3.PositionLessThan200M:
		m = 200
	case layer3.PositionLessThan2KM:
		m = 2000
	case layer3.PositionLessThan20KM:
		m = 20000
	case layer3.PositionLessThan200KM:
		m = 200000
	case layer3.PositionMoreThan200KM, layer3.PositionErrorUnknown:
		return nil
	default:
		return nil
	}
	return &m
}
