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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
)

// PluginTokenPrefix starts every plugin token, so a leaked one is easy to
// recognise.
const PluginTokenPrefix = "dmrhub_plg_" //nolint:gosec // a prefix, not a credential

const (
	pluginTokenBytes = 32
	// MaxDMRID is the largest 24-bit DMR ID.
	MaxDMRID = 1<<24 - 1
	// MaxPluginRanges is the most ID ranges one plugin may have.
	MaxPluginRanges = 32
	// MaxPluginName is the longest name of a plugin.
	MaxPluginName = 64
	// PluginOnline is how long after its last heartbeat a connected plugin
	// still counts as online.
	PluginOnline = 45 * time.Second
)

var (
	// ErrPluginName is returned for an empty or too long plugin name, or one
	// that another plugin has.
	ErrPluginName = errors.New("invalid plugin name")
	// ErrPluginRange is returned for an ID range that is empty, out of the
	// 24-bit DMR IDs, or overlaps another range.
	ErrPluginRange = errors.New("invalid plugin ID range")
	// ErrPluginNotFound is returned for a plugin that does not exist.
	ErrPluginNotFound = errors.New("plugin not found")
)

// Plugin is a program that answers the calls to its IDs over the plugin
// protocol: one that runs as its own program, or a built-in or uploaded
// one that runs inside DMRHub. Only the hash of its token is kept.
type Plugin struct {
	ID              uint      `gorm:"primaryKey"`
	Name            string    `gorm:"size:64;not null;uniqueIndex"`
	TokenHash       []byte    `gorm:"size:32;not null;uniqueIndex"`
	TokenCreatedAt  time.Time `gorm:"not null"`
	Enabled         bool      `gorm:"not null;default:true"`
	LastConnectedAt *time.Time
	LastSeenAt      *time.Time
	LastAddress     string        `gorm:"size:64"`
	Session         string        `gorm:"size:32;not null;default:''"`
	Kind            string        `gorm:"size:16;not null;default:'external'"`
	Builtin         string        `gorm:"size:32;not null;default:''"`
	Version         string        `gorm:"size:32;not null;default:''"`
	Author          string        `gorm:"size:64;not null;default:''"`
	Description     string        `gorm:"size:500;not null;default:''"`
	ModuleHash      []byte        `gorm:"size:32"`
	Ranges          []PluginRange `gorm:"constraint:OnDelete:CASCADE"`
	Hosts           []PluginHost  `gorm:"constraint:OnDelete:CASCADE"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName returns the table of the plugins.
func (Plugin) TableName() string {
	return "plugins"
}

// PluginRange is the DMR IDs from First to Last, both included, that belong
// to a plugin.
type PluginRange struct {
	ID       uint `gorm:"primaryKey"`
	PluginID uint `gorm:"not null;index"`
	First    uint `gorm:"column:first_id;not null"`
	Last     uint `gorm:"column:last_id;not null"`
}

// TableName returns the table of the plugin ID ranges.
func (PluginRange) TableName() string {
	return "plugin_ranges"
}

// Contains reports whether id is in r.
func (r PluginRange) Contains(id uint) bool {
	return id >= r.First && id <= r.Last
}

func hashPluginToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newPluginToken() (string, []byte) {
	var b [pluginTokenBytes]byte
	_, _ = rand.Read(b[:])
	token := PluginTokenPrefix + hex.EncodeToString(b[:])
	return token, hashPluginToken(token)
}

// checkPluginRanges checks that ranges are valid and overlap neither each
// other nor the ranges of the other plugins than self.
func checkPluginRanges(tx *gorm.DB, self uint, ranges []PluginRange) error {
	if len(ranges) > MaxPluginRanges {
		return fmt.Errorf("%w: at most %d ranges", ErrPluginRange, MaxPluginRanges)
	}
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(a, b PluginRange) int { return int(a.First) - int(b.First) })
	for i, r := range sorted {
		if r.First == 0 || r.First > r.Last || r.Last > MaxDMRID {
			return fmt.Errorf("%w: %d to %d", ErrPluginRange, r.First, r.Last)
		}
		if i > 0 && r.First <= sorted[i-1].Last {
			return fmt.Errorf("%w: %d to %d overlaps %d to %d", ErrPluginRange, r.First, r.Last, sorted[i-1].First, sorted[i-1].Last)
		}
	}
	for _, r := range sorted {
		var taken []PluginRange
		err := tx.Where("plugin_id <> ? AND first_id <= ? AND last_id >= ?", self, r.Last, r.First).Limit(1).Find(&taken).Error
		if err != nil {
			return fmt.Errorf("could not check the plugin ID ranges: %w", err)
		}
		if len(taken) > 0 {
			return fmt.Errorf("%w: %d to %d overlaps the IDs of another plugin", ErrPluginRange, r.First, r.Last)
		}
	}
	return nil
}

func checkPluginName(tx *gorm.DB, self uint, name string) error {
	if name == "" || len(name) > MaxPluginName || strings.TrimSpace(name) != name {
		return fmt.Errorf("%w: %q", ErrPluginName, name)
	}
	var n int64
	if err := tx.Model(&Plugin{}).Where("name = ? AND id <> ?", name, self).Count(&n).Error; err != nil {
		return fmt.Errorf("could not check the plugin name: %w", err)
	}
	if n > 0 {
		return fmt.Errorf("%w: another plugin is called %q", ErrPluginName, name)
	}
	return nil
}

func replacePluginRanges(tx *gorm.DB, id uint, ranges []PluginRange) error {
	if err := tx.Where("plugin_id = ?", id).Delete(&PluginRange{}).Error; err != nil {
		return fmt.Errorf("could not replace the plugin ID ranges: %w", err)
	}
	if len(ranges) == 0 {
		return nil
	}
	rows := make([]PluginRange, len(ranges))
	for i, r := range ranges {
		rows[i] = PluginRange{PluginID: id, First: r.First, Last: r.Last}
	}
	if err := tx.Create(&rows).Error; err != nil {
		return fmt.Errorf("could not store the plugin ID ranges: %w", err)
	}
	return nil
}

// CreatePlugin adds a plugin with ranges and returns it with its token, which
// is shown only now.
func CreatePlugin(db *gorm.DB, name string, ranges []PluginRange, now time.Time) (Plugin, string, error) {
	token, hash := newPluginToken()
	p := Plugin{Name: name, TokenHash: hash, TokenCreatedAt: now.UTC(), Enabled: true, Kind: PluginExternal}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := checkPluginName(tx, 0, name); err != nil {
			return err
		}
		if err := checkPluginRanges(tx, 0, ranges); err != nil {
			return err
		}
		if err := tx.Omit("Ranges", "Hosts").Create(&p).Error; err != nil {
			return fmt.Errorf("could not add the plugin: %w", err)
		}
		return replacePluginRanges(tx, p.ID, ranges)
	})
	if err != nil {
		return Plugin{}, "", fmt.Errorf("could not add plugin %q: %w", name, err)
	}
	p, err = FindPlugin(db, p.ID)
	return p, token, err
}

// UpdatePlugin sets the name, whether it is on, and the ranges of the plugin
// id.
func UpdatePlugin(db *gorm.DB, id uint, name string, enabled bool, ranges []PluginRange) (Plugin, error) {
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := FindPlugin(tx, id); err != nil {
			return err
		}
		if err := checkPluginName(tx, id, name); err != nil {
			return err
		}
		if err := checkPluginRanges(tx, id, ranges); err != nil {
			return err
		}
		if err := tx.Model(&Plugin{}).Where("id = ?", id).Select("Name", "Enabled").Updates(Plugin{Name: name, Enabled: enabled}).Error; err != nil {
			return fmt.Errorf("could not update the plugin: %w", err)
		}
		return replacePluginRanges(tx, id, ranges)
	})
	if err != nil {
		return Plugin{}, fmt.Errorf("could not update plugin %d: %w", id, err)
	}
	return FindPlugin(db, id)
}

// RotatePluginToken gives the plugin id a new token, which is shown only
// now. The old token stops working.
func RotatePluginToken(db *gorm.DB, id uint, now time.Time) (string, error) {
	token, hash := newPluginToken()
	res := db.Model(&Plugin{}).Where("id = ?", id).Updates(map[string]any{"token_hash": hash, "token_created_at": now.UTC()})
	if res.Error != nil {
		return "", fmt.Errorf("could not make a new plugin token: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return "", ErrPluginNotFound
	}
	return token, nil
}

// DeletePlugin removes the plugin id and frees its IDs.
func DeletePlugin(db *gorm.DB, id uint) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plugin_id = ?", id).Delete(&PluginRange{}).Error; err != nil {
			return fmt.Errorf("could not delete the plugin ID ranges: %w", err)
		}
		if err := tx.Where("plugin_id = ?", id).Delete(&PluginHost{}).Error; err != nil {
			return fmt.Errorf("could not delete the plugin hosts: %w", err)
		}
		if err := tx.Where("plugin_id = ?", id).Delete(&PluginModule{}).Error; err != nil {
			return fmt.Errorf("could not delete the plugin module: %w", err)
		}
		res := tx.Delete(&Plugin{}, id)
		if res.Error != nil {
			return fmt.Errorf("could not delete the plugin: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrPluginNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not delete plugin %d: %w", id, err)
	}
	return nil
}

// FindPlugin returns the plugin id with its ranges.
func FindPlugin(db *gorm.DB, id uint) (Plugin, error) {
	var found []Plugin
	if err := preloadPlugin(db).Where("id = ?", id).Limit(1).Find(&found).Error; err != nil {
		return Plugin{}, fmt.Errorf("could not find the plugin: %w", err)
	}
	if len(found) == 0 {
		return Plugin{}, ErrPluginNotFound
	}
	return found[0], nil
}

// ListPlugins returns every plugin with its ranges, by name.
func ListPlugins(db *gorm.DB) ([]Plugin, error) {
	var out []Plugin
	if err := preloadPlugin(db).Order("name").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("could not list the plugins: %w", err)
	}
	return out, nil
}

// PluginByToken returns the plugin whose token is token, and false when none
// has it.
func PluginByToken(db *gorm.DB, token string) (Plugin, bool, error) {
	if !strings.HasPrefix(token, PluginTokenPrefix) {
		return Plugin{}, false, nil
	}
	var found []Plugin
	if err := preloadPlugin(db).Where("token_hash = ?", hashPluginToken(token)).Limit(1).Find(&found).Error; err != nil {
		return Plugin{}, false, fmt.Errorf("could not find the plugin of a token: %w", err)
	}
	if len(found) == 0 {
		return Plugin{}, false, nil
	}
	return found[0], true, nil
}

// NotePluginConnected records that the plugin id connected from address in
// session.
func NotePluginConnected(db *gorm.DB, id uint, session, address string, now time.Time) error {
	if len(address) > 64 {
		address = address[:64]
	}
	at := now.UTC()
	err := db.Model(&Plugin{}).Where("id = ?", id).Updates(map[string]any{
		"last_connected_at": at, "last_seen_at": at, "last_address": address, "session": session,
	}).Error
	if err != nil {
		return fmt.Errorf("could not note a plugin connection: %w", err)
	}
	return nil
}

// NotePluginSeen records that the session of the plugin id is still there.
func NotePluginSeen(db *gorm.DB, id uint, session string, now time.Time) error {
	err := db.Model(&Plugin{}).Where("id = ? AND session = ?", id, session).Update("last_seen_at", now.UTC()).Error
	if err != nil {
		return fmt.Errorf("could not note a plugin heartbeat: %w", err)
	}
	return nil
}

// NotePluginDisconnected records that the session of the plugin id ended,
// unless a newer session took over.
func NotePluginDisconnected(db *gorm.DB, id uint, session string, now time.Time) error {
	err := db.Model(&Plugin{}).Where("id = ? AND session = ?", id, session).Updates(map[string]any{"session": "", "last_seen_at": now.UTC()}).Error
	if err != nil {
		return fmt.Errorf("could not note a plugin disconnection: %w", err)
	}
	return nil
}

// Online reports whether the plugin has a session whose heartbeat is recent
// at now.
func (p *Plugin) Online(now time.Time) bool {
	return p.Session != "" && p.LastSeenAt != nil && now.Sub(*p.LastSeenAt) < PluginOnline
}

// CountCallsTo counts the calls to the IDs of ranges since since.
func CountCallsTo(db *gorm.DB, ranges []PluginRange, since time.Time) (int64, error) {
	if len(ranges) == 0 {
		return 0, nil
	}
	q := db.Model(&Call{}).Where("start_time >= ?", since.UTC())
	cond := db.Where("destination_id BETWEEN ? AND ?", ranges[0].First, ranges[0].Last)
	for _, r := range ranges[1:] {
		cond = cond.Or("destination_id BETWEEN ? AND ?", r.First, r.Last)
	}
	var n int64
	if err := q.Where(cond).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("could not count the calls to a plugin: %w", err)
	}
	return n, nil
}
