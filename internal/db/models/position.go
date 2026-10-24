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
	"gorm.io/gorm/clause"
)

// PositionFormat is the protocol that a position arrived in.
type PositionFormat string

// The PositionFormat values.
const (
	PositionFormatLRRP PositionFormat = "lrrp"
	PositionFormatNMEA PositionFormat = "nmea"
	PositionFormatHDAP PositionFormat = "hdap"
)

const (
	// DefaultPositionInterval is how often a tracked radio reports unless
	// its owner picks another interval.
	DefaultPositionInterval = 10 * time.Minute
	// DefaultPositionRetention is how long the positions of a radio are
	// kept unless its owner picks another time.
	DefaultPositionRetention = 24 * time.Hour
	// PositionStoreInterval is the shortest time between two stored
	// positions of one radio.
	PositionStoreInterval = 30 * time.Second

	maxRetentionHours = 365 * 24
)

// PositionSetting is the position tracking of one radio ID, set by its
// owner.
type PositionSetting struct {
	RadioID         uint `gorm:"primaryKey;autoIncrement:false"`
	UserID          uint `gorm:"not null;index"`
	Tracking        bool `gorm:"not null;default:false"`
	TrackingSince   *time.Time
	IntervalSeconds uint   `gorm:"not null;default:600"`
	RetentionHours  uint   `gorm:"not null;default:24"`
	APRS            bool   `gorm:"not null;default:false"`
	APRSSSID        string `gorm:"size:2"`
	APRSSymbol      string `gorm:"size:16"`
	APRSComment     string `gorm:"size:64"`
	UpdatedAt       time.Time
}

// TableName returns the table of the position settings.
func (PositionSetting) TableName() string {
	return "position_settings"
}

// Position is one position that a tracked radio reported.
type Position struct {
	ID         uint           `gorm:"primarykey"`
	RadioID    uint           `gorm:"not null;index:idx_positions_radio_received,priority:1"`
	Latitude   float64        `gorm:"not null"`
	Longitude  float64        `gorm:"not null"`
	Format     PositionFormat `gorm:"size:8;not null"`
	AccuracyM  *float64
	SpeedMPS   *float64
	HeadingDeg *float64
	AltitudeM  *float64
	FixTime    *time.Time
	ReceivedAt time.Time `gorm:"not null;index;index:idx_positions_radio_received,priority:2"`
	RepeaterID uint      `gorm:"not null;default:0"`
	Slot       uint8     `gorm:"not null;default:1"`
	ServiceID  uint      `gorm:"not null;default:0"`
}

// TableName returns the table of the positions.
func (Position) TableName() string {
	return "positions"
}

// FindPositionSetting returns the setting of radioID, and false when its
// owner never set one.
func FindPositionSetting(db *gorm.DB, radioID uint) (PositionSetting, bool, error) {
	var found []PositionSetting
	if err := db.Where("radio_id = ?", radioID).Limit(1).Find(&found).Error; err != nil {
		return PositionSetting{}, false, fmt.Errorf("could not read the position setting of %d: %w", radioID, err)
	}
	if len(found) == 0 {
		return PositionSetting{RadioID: radioID, IntervalSeconds: uint(DefaultPositionInterval / time.Second), RetentionHours: uint(DefaultPositionRetention / time.Hour)}, false, nil
	}
	return found[0], true, nil
}

// SetPositionTracking turns position tracking of radioID, owned by
// userID, on or off. Turning it on when it is off starts it at now;
// turning it off deletes the positions of the radio and stops APRS-IS.
func SetPositionTracking(db *gorm.DB, userID, radioID uint, on bool, now time.Time) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		s, _, err := FindPositionSetting(tx, radioID)
		if err != nil {
			return err
		}
		s.UserID = userID
		switch {
		case on && s.Tracking:
			return nil
		case on:
			at := now.UTC()
			s.Tracking, s.TrackingSince = true, &at
		default:
			s.Tracking, s.TrackingSince, s.APRS = false, nil, false
			if err := tx.Where("radio_id = ?", radioID).Delete(&Position{}).Error; err != nil {
				return fmt.Errorf("could not delete the positions of %d: %w", radioID, err)
			}
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&s).Error
	})
	if err != nil {
		return fmt.Errorf("could not set the position tracking of %d: %w", radioID, err)
	}
	return nil
}

// SetPositionRetention sets how long the positions of radioID are kept.
func SetPositionRetention(db *gorm.DB, radioID uint, keep time.Duration) error {
	hours := max(keep/time.Hour, 1)
	err := db.Model(&PositionSetting{}).Where("radio_id = ?", radioID).Update("retention_hours", int64(hours)).Error
	if err != nil {
		return fmt.Errorf("could not set the position retention of %d: %w", radioID, err)
	}
	return nil
}

// PositionOptions is what the owner of a radio picks besides turning
// tracking on or off.
type PositionOptions struct {
	Interval    time.Duration
	Retention   time.Duration
	APRS        bool
	APRSSSID    string
	APRSSymbol  string
	APRSComment string
}

// SetPositionOptions sets the options of radioID, owned by userID, and
// keeps whether it is tracked.
func SetPositionOptions(db *gorm.DB, userID, radioID uint, o PositionOptions) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		s, _, err := FindPositionSetting(tx, radioID)
		if err != nil {
			return err
		}
		s.UserID = userID
		s.IntervalSeconds = uint(max(o.Interval/time.Second, 0))
		s.RetentionHours = uint(max(o.Retention/time.Hour, 1))
		s.APRS, s.APRSSSID, s.APRSSymbol, s.APRSComment = o.APRS && s.Tracking, o.APRSSSID, o.APRSSymbol, o.APRSComment
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&s).Error
	})
	if err != nil {
		return fmt.Errorf("could not set the position options of %d: %w", radioID, err)
	}
	return nil
}

// SetSharePositions sets whether the user userID shares the positions of
// their radios with the other users who share theirs.
func SetSharePositions(db *gorm.DB, userID uint, share bool) error {
	if err := db.Exec("UPDATE users SET share_positions = ? WHERE id = ?", share, userID).Error; err != nil {
		return fmt.Errorf("could not set position sharing of %d: %w", userID, err)
	}
	return nil
}

// StorePosition stores p when its radio is tracked, p arrived after
// tracking started, and no position of the radio was stored in the
// PositionStoreInterval before p. It reports whether it stored p.
func StorePosition(db *gorm.DB, p Position) (bool, error) {
	s, ok, err := FindPositionSetting(db, p.RadioID)
	if err != nil || !ok || !s.Tracking || s.TrackingSince == nil || p.ReceivedAt.Before(*s.TrackingSince) {
		return false, err
	}
	var recent int64
	err = db.Model(&Position{}).
		Where("radio_id = ? AND received_at > ?", p.RadioID, p.ReceivedAt.Add(-PositionStoreInterval).UTC()).
		Count(&recent).Error
	if err != nil {
		return false, fmt.Errorf("could not check the last position of %d: %w", p.RadioID, err)
	}
	if recent > 0 {
		return false, nil
	}
	p.ReceivedAt = p.ReceivedAt.UTC()
	if err := db.Create(&p).Error; err != nil {
		return false, fmt.Errorf("could not store a position of %d: %w", p.RadioID, err)
	}
	return true, nil
}

// LatestPositions returns the newest position after since of each radio
// that viewer may see: their own, and when they share, those of the
// other users who share.
func LatestPositions(db *gorm.DB, viewer uint, since time.Time) ([]Position, error) {
	var share bool
	if err := db.Model(&User{}).Where("id = ?", viewer).Select("share_positions").Scan(&share).Error; err != nil {
		return nil, fmt.Errorf("could not read position sharing of %d: %w", viewer, err)
	}
	owners := db.Model(&PositionSetting{}).Select("radio_id").Where("user_id = ?", viewer)
	if share {
		owners = db.Model(&PositionSetting{}).Select("position_settings.radio_id").
			Joins("JOIN users ON users.id = position_settings.user_id").
			Where("position_settings.user_id = ? OR users.share_positions = ?", viewer, true)
	}
	newest := db.Model(&Position{}).Select("radio_id, MAX(received_at) AS received_at").
		Where("received_at >= ? AND radio_id IN (?)", since.UTC(), owners).Group("radio_id")
	var out []Position
	err := db.Joins("JOIN (?) AS newest ON newest.radio_id = positions.radio_id AND newest.received_at = positions.received_at", newest).
		Order("positions.radio_id").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("could not list positions: %w", err)
	}
	return out, nil
}

// PrunePositions deletes the positions older than the retention of their
// radio, and those older than most whatever the retention of their radio.
func PrunePositions(db *gorm.DB, now time.Time, most time.Duration) error {
	var settings []PositionSetting
	if err := db.Select("radio_id", "retention_hours").Find(&settings).Error; err != nil {
		return fmt.Errorf("could not list position settings: %w", err)
	}
	for _, s := range settings {
		keep := DefaultPositionRetention
		if s.RetentionHours > 0 && s.RetentionHours <= maxRetentionHours {
			keep = time.Duration(s.RetentionHours) * time.Hour
		}
		if most > 0 {
			keep = min(keep, most)
		}
		if err := db.Where("radio_id = ? AND received_at < ?", s.RadioID, now.Add(-keep).UTC()).Delete(&Position{}).Error; err != nil {
			return fmt.Errorf("could not prune the positions of %d: %w", s.RadioID, err)
		}
	}
	return nil
}

// PositionOwner returns the user who tracks radioID, and false when no one
// does.
func PositionOwner(db *gorm.DB, radioID uint) (uint, bool, error) {
	s, ok, err := FindPositionSetting(db, radioID)
	if err != nil || !ok || !s.Tracking {
		return 0, false, err
	}
	return s.UserID, true, nil
}

// SeesPositions reports whether viewer may see the positions of radioID:
// they track it, or they and its owner both share their positions.
func SeesPositions(db *gorm.DB, viewer, radioID uint) (bool, error) {
	owner, ok, err := PositionOwner(db, radioID)
	if err != nil || !ok {
		return false, err
	}
	if owner == viewer {
		return true, nil
	}
	var n int64
	err = db.Model(&User{}).Where("id IN ? AND share_positions = ?", []uint{viewer, owner}, true).Count(&n).Error
	if err != nil {
		return false, fmt.Errorf("could not read position sharing: %w", err)
	}
	return n == 2, nil
}

// TracksPositions reports whether userID tracks the position of any radio.
func TracksPositions(db *gorm.DB, userID uint) (bool, error) {
	var n int64
	if err := db.Model(&PositionSetting{}).Where("user_id = ? AND tracking = ?", userID, true).Count(&n).Error; err != nil {
		return false, fmt.Errorf("could not read position tracking of %d: %w", userID, err)
	}
	return n > 0, nil
}

// Track returns the positions of radioID received between from and to, oldest
// first.
func Track(db *gorm.DB, radioID uint, from, to time.Time) ([]Position, error) {
	var out []Position
	err := db.Where("radio_id = ? AND received_at >= ? AND received_at <= ?", radioID, from.UTC(), to.UTC()).
		Order("received_at").Order("id").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("could not read the track of %d: %w", radioID, err)
	}
	return out, nil
}
