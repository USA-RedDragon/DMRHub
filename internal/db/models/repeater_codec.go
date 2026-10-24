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

package models

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// TalkgroupRepeaterIDs returns the IDs of the repeaters that have the
// talkgroup tg on a timeslot, static or dynamic.
func TalkgroupRepeaterIDs(db *gorm.DB, tg uint) ([]uint, error) {
	var ids []uint
	err := db.Model(&Repeater{}).
		Where("ts1_dynamic_talkgroup_id = ? OR ts2_dynamic_talkgroup_id = ?", tg, tg).
		Or("id IN (?)", db.Table("repeater_ts1_static_talkgroups").Select("repeater_id").Where("talkgroup_id = ?", tg)).
		Or("id IN (?)", db.Table("repeater_ts2_static_talkgroups").Select("repeater_id").Where("talkgroup_id = ?", tg)).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("listing the repeaters of talkgroup %d: %w", tg, err)
	}
	return ids, nil
}

// Codec2RepeaterIDs returns the IDs of the repeaters with a timeslot that
// SlotCodec makes Codec2.
func Codec2RepeaterIDs(db *gorm.DB) ([]uint, error) {
	var ids []uint
	err := db.Model(&Repeater{}).
		Where("type <> ?", RepeaterTypeIPSC).
		Where(db.Where("ts1_codec = ? OR ts2_codec = ?", CodecCodec2, CodecCodec2).
			Or("ts1_codec = ? AND ts1_heard_codec = ?", CodecSettingAuto, CodecCodec2).
			Or("ts2_codec = ? AND ts2_heard_codec = ?", CodecSettingAuto, CodecCodec2)).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("listing the repeaters with a Codec2 timeslot: %w", err)
	}
	return ids, nil
}

// SetRepeaterHeardCodec records the codec of a call that a timeslot of
// the repeater id heard at the time at, slot true for TS2. A mode other
// than 0 is recorded as the Codec2 mode of the slot.
func SetRepeaterHeardCodec(db *gorm.DB, id uint, slot bool, codec string, mode int, at time.Time) error {
	q := "UPDATE repeaters SET ts1_heard_codec = ?, ts1_heard_at = ? WHERE id = ?"
	qm := "UPDATE repeaters SET ts1_heard_codec = ?, ts1_heard_at = ?, ts1_heard_codec2_mode = ? WHERE id = ?"
	if slot {
		q = "UPDATE repeaters SET ts2_heard_codec = ?, ts2_heard_at = ? WHERE id = ?"
		qm = "UPDATE repeaters SET ts2_heard_codec = ?, ts2_heard_at = ?, ts2_heard_codec2_mode = ? WHERE id = ?"
	}
	var err error
	if mode != 0 {
		err = db.Exec(qm, codec, at, mode, id).Error
	} else {
		err = db.Exec(q, codec, at, id).Error
	}
	if err != nil {
		return fmt.Errorf("recording the codec heard on repeater %d: %w", id, err)
	}
	return nil
}

// SetRepeaterCodec sets the codec setting of the timeslots of the
// repeater id to ts1 and ts2.
func SetRepeaterCodec(db *gorm.DB, id uint, ts1, ts2 string) error {
	if err := db.Exec("UPDATE repeaters SET ts1_codec = ?, ts2_codec = ? WHERE id = ?", ts1, ts2, id).Error; err != nil {
		return fmt.Errorf("setting the codec of repeater %d: %w", id, err)
	}
	return nil
}
