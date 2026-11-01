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

package plugins

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"gorm.io/gorm"
)

const (
	// TrackingTopic is the pubsub topic that says that the owner of a radio changed its tracking.
	TrackingTopic = "plugins:tracking"
	// HeardTopic is the pubsub topic that says where a radio was heard,
	// with no time. Replicas older than HeardAtTopic read only it.
	HeardTopic = "plugins:heard"
	// HeardAtTopic is the pubsub topic that says where and when a radio was
	// heard. A replica that publishes on it publishes the same sighting on
	// HeardTopic after it, until no replica older than HeardAtTopic runs.
	HeardAtTopic = "plugins:heard:2"
	// heardSize is the size of a message on HeardTopic: the radio, the
	// repeater, the slot and the color code.
	heardSize   = 10
	heardAtSize = heardSize + 8
	twinWait    = 10 * time.Second
)

// PublishTracking tells the plugins on every replica that the owner of
// radio changed its tracking.
func PublishTracking(ps pubsub.PubSub, radio uint) error {
	if err := ps.Publish(TrackingTopic, []byte(strconv.FormatUint(uint64(radio), 10))); err != nil {
		return fmt.Errorf("could not publish a tracking change: %w", err)
	}
	return nil
}

type heardKey struct {
	radio, repeater uint32
	slot, cc        uint8
}

// heard is where and when a radio was heard. at is in Unix milliseconds,
// and 0 when the message held no time.
type heard struct {
	heardKey
	at int64
}

func (h heardKey) append(b []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, h.radio)
	b = binary.BigEndian.AppendUint32(b, h.repeater)
	return append(b, h.slot, h.cc)
}

func keyOf(radio, repeater uint, slot, cc uint8) heardKey {
	return heardKey{radio: uint32(radio), repeater: uint32(repeater), slot: slot, cc: cc} //nolint:gosec // DMR IDs are 24 bits, repeater IDs at most 9 digits
}

// PublishHeard tells the plugins on every replica that radio was heard on
// repeater, on slot 1 or 2, whose color code is cc, with no time.
func PublishHeard(ps pubsub.PubSub, radio, repeater uint, slot, cc uint8) error {
	if err := ps.Publish(HeardTopic, keyOf(radio, repeater, slot, cc).append(make([]byte, 0, heardSize))); err != nil {
		return fmt.Errorf("could not publish where a radio was heard: %w", err)
	}
	return nil
}

// PublishHeardAt tells the plugins on the replicas that read HeardAtTopic
// that radio was heard on repeater, on slot 1 or 2, whose color code is
// cc, at the time at. Publish the same sighting with PublishHeard right
// after it.
func PublishHeardAt(ps pubsub.PubSub, radio, repeater uint, slot, cc uint8, at time.Time) error {
	b := keyOf(radio, repeater, slot, cc).append(make([]byte, 0, heardAtSize))
	b = binary.BigEndian.AppendUint64(b, uint64(at.UnixMilli())) //nolint:gosec // a time after 1970
	if err := ps.Publish(HeardAtTopic, b); err != nil {
		return fmt.Errorf("could not publish when a radio was heard: %w", err)
	}
	return nil
}

// DecodeHeard returns the radio, repeater, slot, color code and time of a
// message on topic, HeardTopic or HeardAtTopic, and false when it is not
// one. The time is zero for a message on HeardTopic.
func DecodeHeard(topic string, b []byte) (radio, repeater uint, slot, cc uint8, at time.Time, ok bool) {
	h, ok := decodeHeard(topic, b)
	if ok && h.at != 0 {
		at = time.UnixMilli(h.at)
	}
	return uint(h.radio), uint(h.repeater), h.slot, h.cc, at, ok
}

func decodeHeard(topic string, b []byte) (heard, bool) {
	size := heardSize
	if topic == HeardAtTopic {
		size = heardAtSize
	}
	if len(b) != size || (topic != HeardTopic && topic != HeardAtTopic) {
		return heard{}, false
	}
	h := heard{heardKey: heardKey{radio: binary.BigEndian.Uint32(b[0:]), repeater: binary.BigEndian.Uint32(b[4:]), slot: b[8], cc: b[9]}}
	if size == heardAtSize {
		h.at = int64(binary.BigEndian.Uint64(b[heardSize:])) //nolint:gosec // written from an int64
	}
	return h, true
}

// trackingOf returns the tracking of the radio in s.
func trackingOf(s *models.PositionSetting) *pluginv1.Tracking {
	return &pluginv1.Tracking{Radio: uint32(s.RadioID), On: s.Tracking, IntervalSeconds: uint32(s.IntervalSeconds)} //nolint:gosec // DMR IDs are 24 bits, intervals at most an hour
}

// tracked returns the radios with tracking on.
func tracked(db *gorm.DB) ([]*pluginv1.Tracking, error) {
	var settings []models.PositionSetting
	if err := db.Where("tracking = ?", true).Order("radio_id").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("could not list the tracked radios: %w", err)
	}
	out := make([]*pluginv1.Tracking, len(settings))
	for i := range settings {
		out[i] = trackingOf(&settings[i])
	}
	return out, nil
}

// positionOf returns the position p that the plugin with ranges sent, heard
// last on repeater, received at now, and false when the plugin may not send
// it: its service is not one of the plugin's IDs, or it is not a place on
// Earth.
func positionOf(p *pluginv1.Position, ranges []models.PluginRange, repeater uint, now time.Time) (models.Position, bool) {
	lat, lon := p.GetLatitude(), p.GetLongitude()
	if !inRanges(ranges, uint(p.GetService())) || p.GetRadio() == 0 ||
		math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 || (lat == 0 && lon == 0) {
		return models.Position{}, false
	}
	pos := models.Position{
		RadioID: uint(p.GetRadio()), Latitude: lat, Longitude: lon, ReceivedAt: now,
		RepeaterID: repeater, Slot: 1, ServiceID: uint(p.GetService()),
	}
	if p.GetSlot() == 2 {
		pos.Slot = 2
	}
	switch p.GetFormat() {
	case pluginv1.PositionFormat_POSITION_FORMAT_LRRP:
		pos.Format = models.PositionFormatLRRP
	case pluginv1.PositionFormat_POSITION_FORMAT_NMEA:
		pos.Format = models.PositionFormatNMEA
	case pluginv1.PositionFormat_POSITION_FORMAT_HDAP:
		pos.Format = models.PositionFormatHDAP
	case pluginv1.PositionFormat_POSITION_FORMAT_UNSPECIFIED:
		return models.Position{}, false
	}
	pos.AccuracyM = finite(p.AccuracyMeters)
	pos.SpeedMPS = finite(p.SpeedMetersPerSecond)
	pos.HeadingDeg = finite(p.HeadingDegrees)
	pos.AltitudeM = finite(p.AltitudeMeters)
	if p.FixTime != nil {
		at := time.Unix(p.GetFixTime(), 0).UTC()
		pos.FixTime = &at
	}
	return pos, true
}

func finite(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return v
}
