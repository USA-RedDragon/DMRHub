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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The limits of a plugin's store and switches.
const (
	// MaxPluginStoreKey is the longest key of a plugin's store, in bytes.
	MaxPluginStoreKey = 255
	// MaxPluginStoreValue is the largest value of a plugin's store, in
	// bytes.
	MaxPluginStoreValue = 16384
	// MaxPluginStoreKeys is how many keys a plugin's store holds.
	MaxPluginStoreKeys = 10000
	// MaxPluginStoreList is how many keys one list returns.
	MaxPluginStoreList = 100
	// MaxPluginSwitches is how many switches a plugin declares.
	MaxPluginSwitches = 4
	// MaxPluginSwitchKey is the longest key of a switch.
	MaxPluginSwitchKey = 32
	// MaxPluginSwitchChoice is the longest option of a switch.
	MaxPluginSwitchChoice = 16
)

var (
	// ErrPluginStoreKey is returned for a key that is empty or too long.
	ErrPluginStoreKey = errors.New("invalid plugin store key")
	// ErrPluginStoreValue is returned for a value that is too large.
	ErrPluginStoreValue = errors.New("plugin store value too large")
	// ErrPluginStoreFull is returned for a new key of a full store.
	ErrPluginStoreFull = errors.New("the plugin store is full")
	// ErrPluginSwitch is returned for a switch that a plugin cannot
	// declare, or a user setting of a switch or choice that it does not
	// have.
	ErrPluginSwitch = errors.New("invalid plugin switch")
)

// PluginStoreEntry is a key and its value in the store of a plugin. The key
// is kept in hexadecimal, so that every database compares and orders keys
// by their bytes.
type PluginStoreEntry struct {
	PluginID  uint       `gorm:"primaryKey;autoIncrement:false"`
	KeyHex    string     `gorm:"primaryKey;size:510"`
	Value     []byte     `gorm:"not null"`
	ExpiresAt *time.Time `gorm:"index"`
	UpdatedAt time.Time
}

// TableName returns the table of the plugin stores.
func (PluginStoreEntry) TableName() string {
	return "plugin_store_entries"
}

// Key returns the key of e.
func (e *PluginStoreEntry) Key() string {
	b, _ := hex.DecodeString(e.KeyHex)
	return string(b)
}

func checkStoreKey(key string) error {
	if key == "" || len(key) > MaxPluginStoreKey {
		return ErrPluginStoreKey
	}
	return nil
}

func liveEntries(db *gorm.DB, plugin uint, now time.Time) *gorm.DB {
	return db.Where("plugin_id = ? AND (expires_at IS NULL OR expires_at > ?)", plugin, now.UTC())
}

// PluginStoreGet returns the entry of key in the store of plugin, and false
// when there is none or it expired by now.
func PluginStoreGet(db *gorm.DB, plugin uint, key string, now time.Time) (PluginStoreEntry, bool, error) {
	if err := checkStoreKey(key); err != nil {
		return PluginStoreEntry{}, false, err
	}
	var found []PluginStoreEntry
	if err := liveEntries(db, plugin, now).Where("key_hex = ?", hex.EncodeToString([]byte(key))).Limit(1).Find(&found).Error; err != nil {
		return PluginStoreEntry{}, false, fmt.Errorf("could not read the plugin store: %w", err)
	}
	if len(found) == 0 {
		return PluginStoreEntry{}, false, nil
	}
	return found[0], true, nil
}

// PluginStorePut sets the value of key in the store of plugin, to expire at
// expires unless it is nil. It drops the expired keys of the store first.
// It returns ErrPluginStoreFull for a new key of a store that holds
// MaxPluginStoreKeys.
func PluginStorePut(db *gorm.DB, plugin uint, key string, value []byte, expires *time.Time, now time.Time) error {
	if err := checkStoreKey(key); err != nil {
		return err
	}
	if len(value) > MaxPluginStoreValue {
		return ErrPluginStoreValue
	}
	if value == nil {
		value = []byte{}
	}
	if expires != nil {
		at := expires.UTC()
		expires = &at
	}
	k := hex.EncodeToString([]byte(key))
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plugin_id = ? AND expires_at <= ?", plugin, now.UTC()).Delete(&PluginStoreEntry{}).Error; err != nil {
			return fmt.Errorf("could not drop the expired keys: %w", err)
		}
		var n int64
		if err := tx.Model(&PluginStoreEntry{}).Where("plugin_id = ? AND key_hex <> ?", plugin, k).Count(&n).Error; err != nil {
			return fmt.Errorf("could not count the keys: %w", err)
		}
		if n >= MaxPluginStoreKeys {
			return ErrPluginStoreFull
		}
		e := PluginStoreEntry{PluginID: plugin, KeyHex: k, Value: value, ExpiresAt: expires, UpdatedAt: now.UTC()}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&e).Error
	})
	if err != nil {
		return fmt.Errorf("could not write the plugin store: %w", err)
	}
	return nil
}

// PluginStoreDelete drops key from the store of plugin.
func PluginStoreDelete(db *gorm.DB, plugin uint, key string) error {
	if err := checkStoreKey(key); err != nil {
		return err
	}
	if err := db.Where("plugin_id = ? AND key_hex = ?", plugin, hex.EncodeToString([]byte(key))).Delete(&PluginStoreEntry{}).Error; err != nil {
		return fmt.Errorf("could not delete from the plugin store: %w", err)
	}
	return nil
}

// PluginStoreList returns at most limit entries of the store of plugin
// whose keys start with prefix and come after after, in byte order, and
// whether more follow. A limit of 0 or more than MaxPluginStoreList is
// MaxPluginStoreList.
func PluginStoreList(db *gorm.DB, plugin uint, prefix, after string, limit int, now time.Time) ([]PluginStoreEntry, bool, error) {
	if limit <= 0 || limit > MaxPluginStoreList {
		limit = MaxPluginStoreList
	}
	q := liveEntries(db, plugin, now)
	if prefix != "" {
		q = q.Where("key_hex LIKE ?", hex.EncodeToString([]byte(prefix))+"%")
	}
	if after != "" {
		q = q.Where("key_hex > ?", hex.EncodeToString([]byte(after)))
	}
	var found []PluginStoreEntry
	if err := q.Order("key_hex").Limit(limit + 1).Find(&found).Error; err != nil {
		return nil, false, fmt.Errorf("could not list the plugin store: %w", err)
	}
	if len(found) > limit {
		return found[:limit], true, nil
	}
	return found, false, nil
}

// PluginSwitchDetail is a line that the card of a switch shows while it is
// on. Value may hold {callsign}, {choice} and {id}.
type PluginSwitchDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PluginSwitchChoice is a choice that a user makes for a switch.
type PluginSwitchChoice struct {
	Label   string   `json:"label"`
	Options []string `json:"options"`
	Preset  string   `json:"preset"`
}

// PluginSwitchSpec is a per-user switch that a plugin declares.
type PluginSwitchSpec struct {
	Key         string               `json:"key"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Notice      string               `json:"notice"`
	Details     []PluginSwitchDetail `json:"details"`
	Choice      *PluginSwitchChoice  `json:"choice,omitempty"`
}

// Preset returns the option of a user who did not choose, or "" for a
// switch with no choice.
func (s *PluginSwitchSpec) Preset() string {
	if s.Choice == nil || len(s.Choice.Options) == 0 {
		return ""
	}
	if s.Choice.Preset != "" {
		return s.Choice.Preset
	}
	return s.Choice.Options[0]
}

// HasOption reports whether choice is an option of s, or "" for a switch
// with no choice.
func (s *PluginSwitchSpec) HasOption(choice string) bool {
	if s.Choice == nil {
		return choice == ""
	}
	for _, o := range s.Choice.Options {
		if o == choice {
			return true
		}
	}
	return false
}

func validSwitchKey(key string) bool {
	if key == "" || len(key) > MaxPluginSwitchKey {
		return false
	}
	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// Check returns ErrPluginSwitch when s breaks a limit of the protocol.
func (s *PluginSwitchSpec) Check() error {
	bad := func(what string) error { return fmt.Errorf("%w %q: %s", ErrPluginSwitch, s.Key, what) }
	switch {
	case !validSwitchKey(s.Key):
		return bad("the key is not 1 to 32 of a-z, 0-9, _ and -")
	case s.Title == "" || len(s.Title) > 64:
		return bad("the title is not 1 to 64 characters")
	case len(s.Description) > 300 || len(s.Notice) > 300:
		return bad("the description or notice is over 300 characters")
	case len(s.Details) > 4:
		return bad("more than 4 details")
	}
	for _, d := range s.Details {
		if len(d.Label) > 32 || len(d.Value) > 200 {
			return bad("a detail is too long")
		}
	}
	if c := s.Choice; c != nil {
		if len(c.Label) > 32 || len(c.Options) == 0 || len(c.Options) > 32 {
			return bad("the choice needs a label of at most 32 characters and 1 to 32 options")
		}
		seen := map[string]bool{}
		for _, o := range c.Options {
			if o == "" || len(o) > MaxPluginSwitchChoice || seen[o] {
				return bad("an option is empty, over 16 characters or twice")
			}
			seen[o] = true
		}
		if c.Preset != "" && !seen[c.Preset] {
			return bad("the preset is not an option")
		}
	}
	return nil
}

// PluginSwitch is a switch that a plugin declared in its hello.
type PluginSwitch struct {
	PluginID uint   `gorm:"primaryKey;autoIncrement:false"`
	Key      string `gorm:"primaryKey;size:32"`
	Position int    `gorm:"not null;default:0"`
	Spec     string `gorm:"type:text;not null"`
}

// TableName returns the table of the plugins' switches.
func (PluginSwitch) TableName() string {
	return "plugin_switches"
}

// PluginUserSwitch is how a user set a switch of a plugin.
type PluginUserSwitch struct {
	PluginID  uint   `gorm:"primaryKey;autoIncrement:false"`
	UserID    uint   `gorm:"primaryKey;autoIncrement:false;index"`
	Key       string `gorm:"primaryKey;size:32"`
	On        bool   `gorm:"column:is_on;not null;default:false"`
	Choice    string `gorm:"size:16;not null;default:''"`
	UpdatedAt time.Time
}

// TableName returns the table of the users' plugin switches.
func (PluginUserSwitch) TableName() string {
	return "plugin_user_switches"
}

// SetPluginSwitches replaces the switches that plugin declares with specs,
// in their order. The users keep their settings of a switch that goes and
// comes back. Replicas that set the same switches at once all succeed. It
// returns ErrPluginNotFound when there is no such plugin.
func SetPluginSwitches(db *gorm.DB, plugin uint, specs []PluginSwitchSpec) error {
	if len(specs) > MaxPluginSwitches {
		return fmt.Errorf("%w: more than %d switches", ErrPluginSwitch, MaxPluginSwitches)
	}
	rows := make([]PluginSwitch, 0, len(specs))
	seen := map[string]bool{}
	for i := range specs {
		if err := specs[i].Check(); err != nil {
			return err
		}
		if seen[specs[i].Key] {
			return fmt.Errorf("%w %q: twice", ErrPluginSwitch, specs[i].Key)
		}
		seen[specs[i].Key] = true
		b, err := json.Marshal(&specs[i])
		if err != nil {
			return fmt.Errorf("could not encode a switch: %w", err)
		}
		rows = append(rows, PluginSwitch{PluginID: plugin, Key: specs[i].Key, Position: i, Spec: string(b)})
	}
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, r.Key)
	}
	slices.SortFunc(rows, func(a, b PluginSwitch) int { return strings.Compare(a.Key, b.Key) })
	err := db.Transaction(func(tx *gorm.DB) error {
		return putSwitches(tx, plugin, keys, rows)
	})
	if err != nil {
		return fmt.Errorf("could not set the switches of plugin %d: %w", plugin, err)
	}
	return nil
}

// switchKeyColumn is the column of the key of a switch, which gorm quotes,
// as MySQL reserves its name.
const switchKeyColumn = "key"

// putSwitches locks the row of plugin first, so that the saves of its
// switches on several replicas go one after the other.
func putSwitches(tx *gorm.DB, plugin uint, keys []string, rows []PluginSwitch) error {
	var locked []Plugin
	if err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Select("id").Where("id = ?", plugin).Find(&locked).Error; err != nil {
		return fmt.Errorf("could not lock the plugin: %w", err)
	}
	if len(locked) == 0 {
		return ErrPluginNotFound
	}
	drop := tx.Where("plugin_id = ?", plugin)
	if len(keys) > 0 {
		drop = drop.Not(map[string]any{switchKeyColumn: keys})
	}
	if err := drop.Delete(&PluginSwitch{}).Error; err != nil {
		return fmt.Errorf("could not drop the switches: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&rows).Error; err != nil {
		return fmt.Errorf("could not write the switches: %w", err)
	}
	return nil
}

// PluginSwitchesOf returns the switches that plugin declared, in order.
func PluginSwitchesOf(db *gorm.DB, plugin uint) ([]PluginSwitchSpec, error) {
	var rows []PluginSwitch
	if err := db.Where("plugin_id = ?", plugin).Order("position").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("could not list the switches of plugin %d: %w", plugin, err)
	}
	return specsOf(rows), nil
}

func specsOf(rows []PluginSwitch) []PluginSwitchSpec {
	out := make([]PluginSwitchSpec, 0, len(rows))
	for _, r := range rows {
		var s PluginSwitchSpec
		if json.Unmarshal([]byte(r.Spec), &s) == nil && s.Key == r.Key {
			out = append(out, s)
		}
	}
	return out
}

// UserPluginSwitch is a switch of a plugin with how one user set it, its
// choice or the preset when the user did not choose.
type UserPluginSwitch struct {
	Plugin   Plugin
	Spec     PluginSwitchSpec
	UserID   uint
	Callsign string
	On       bool
	Choice   string
}

// UserPluginSwitches returns the switches of plugin with how user set
// them.
func UserPluginSwitches(db *gorm.DB, plugin uint, user *User) ([]UserPluginSwitch, error) {
	specs, err := PluginSwitchesOf(db, plugin)
	if err != nil || len(specs) == 0 {
		return nil, err
	}
	var set []PluginUserSwitch
	if err := db.Where("plugin_id = ? AND user_id = ?", plugin, user.ID).Find(&set).Error; err != nil {
		return nil, fmt.Errorf("could not read the switches of user %d: %w", user.ID, err)
	}
	byKey := map[string]PluginUserSwitch{}
	for _, s := range set {
		byKey[s.Key] = s
	}
	out := make([]UserPluginSwitch, 0, len(specs))
	for _, spec := range specs {
		u := UserPluginSwitch{Plugin: Plugin{ID: plugin}, Spec: spec, UserID: user.ID, Callsign: user.Callsign, Choice: spec.Preset()}
		if s, ok := byKey[spec.Key]; ok {
			u.On = s.On
			if s.Choice != "" && spec.HasOption(s.Choice) {
				u.Choice = s.Choice
			}
		}
		out = append(out, u)
	}
	return out, nil
}

// ListUserPluginSwitches returns the switches of every plugin that is on,
// with how user set them, by plugin ID and then in each plugin's order.
func ListUserPluginSwitches(db *gorm.DB, user *User) ([]UserPluginSwitch, error) {
	var plugins []Plugin
	err := preloadPlugin(db).Where("enabled = ? AND id IN (?)", true, db.Model(&PluginSwitch{}).Select("plugin_id")).Order("id").Find(&plugins).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the plugins with switches: %w", err)
	}
	var out []UserPluginSwitch
	for i := range plugins {
		got, err := UserPluginSwitches(db, plugins[i].ID, user)
		if err != nil {
			return nil, err
		}
		for j := range got {
			got[j].Plugin = plugins[i]
		}
		out = append(out, got...)
	}
	return out, nil
}

// SetUserPluginSwitch sets the switch key of plugin for user on or off,
// with choice, or the preset when choice is "". It returns ErrPluginSwitch
// when the plugin declares no such switch or option.
func SetUserPluginSwitch(db *gorm.DB, plugin, user uint, key string, on bool, choice string, now time.Time) (UserPluginSwitch, error) {
	specs, err := PluginSwitchesOf(db, plugin)
	if err != nil {
		return UserPluginSwitch{}, err
	}
	var spec *PluginSwitchSpec
	for i := range specs {
		if specs[i].Key == key {
			spec = &specs[i]
		}
	}
	if spec == nil {
		return UserPluginSwitch{}, fmt.Errorf("%w: plugin %d has no switch %q", ErrPluginSwitch, plugin, key)
	}
	if choice == "" {
		choice = spec.Preset()
	}
	if !spec.HasOption(choice) {
		return UserPluginSwitch{}, fmt.Errorf("%w: %q is not an option of %q", ErrPluginSwitch, choice, key)
	}
	row := PluginUserSwitch{PluginID: plugin, UserID: user, Key: key, On: on, Choice: choice, UpdatedAt: now.UTC()}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		return UserPluginSwitch{}, fmt.Errorf("could not set a plugin switch: %w", err)
	}
	u, err := FindUserByID(db, user)
	if err != nil {
		return UserPluginSwitch{}, fmt.Errorf("could not find user %d: %w", user, err)
	}
	return UserPluginSwitch{Plugin: Plugin{ID: plugin}, Spec: *spec, UserID: user, Callsign: u.Callsign, On: on, Choice: choice}, nil
}

// PluginSwitchesOn returns each switch of plugin that a user who is
// approved and not suspended turned on, in the plugin's order of its
// switches and then by user.
func PluginSwitchesOn(db *gorm.DB, plugin uint) ([]UserPluginSwitch, error) {
	specs, err := PluginSwitchesOf(db, plugin)
	if err != nil || len(specs) == 0 {
		return nil, err
	}
	var rows []struct {
		PluginUserSwitch
		Callsign string
	}
	err = db.Table("plugin_user_switches").
		Select("plugin_user_switches.*, users.callsign").
		Joins("JOIN users ON users.id = plugin_user_switches.user_id AND users.deleted_at IS NULL").
		Where("plugin_user_switches.plugin_id = ? AND plugin_user_switches.is_on = ? AND users.approved = ? AND users.suspended = ?", plugin, true, true, false).
		Order("plugin_user_switches.user_id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the users of plugin %d: %w", plugin, err)
	}
	out := make([]UserPluginSwitch, 0, len(rows))
	for _, spec := range specs {
		for _, r := range rows {
			if r.Key != spec.Key {
				continue
			}
			choice := r.Choice
			if !spec.HasOption(choice) || choice == "" {
				choice = spec.Preset()
			}
			out = append(out, UserPluginSwitch{Plugin: Plugin{ID: plugin}, Spec: spec, UserID: r.UserID, Callsign: r.Callsign, On: true, Choice: choice})
		}
	}
	return out, nil
}

// PluginSwitchCallsigns returns, in upper case and sorted, the callsigns of
// the approved users who are not suspended and turned on a switch of a
// plugin that is enabled.
func PluginSwitchCallsigns(db *gorm.DB) ([]string, error) {
	var plugins []uint
	err := db.Model(&Plugin{}).Where("enabled = ? AND id IN (?)", true, db.Model(&PluginSwitch{}).Select("plugin_id")).Order("id").Pluck("id", &plugins).Error
	if err != nil {
		return nil, fmt.Errorf("could not list the plugins with switches: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range plugins {
		on, err := PluginSwitchesOn(db, id)
		if err != nil {
			return nil, err
		}
		for i := range on {
			seen[strings.ToUpper(on[i].Callsign)] = true
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	slices.Sort(out)
	return out, nil
}
