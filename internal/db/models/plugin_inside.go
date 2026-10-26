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
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// The kinds of plugins.
const (
	// PluginExternal is a plugin that runs as its own program and connects
	// to the plugin listener.
	PluginExternal = "external"
	// PluginBuiltin is a plugin that comes with DMRHub and runs inside it.
	PluginBuiltin = "builtin"
	// PluginUploaded is a plugin that an admin uploaded, which runs inside
	// DMRHub.
	PluginUploaded = "uploaded"
)

const (
	// MaxPluginVersion is the longest version of a plugin.
	MaxPluginVersion = 32
	// MaxPluginAuthor is the longest author of a plugin.
	MaxPluginAuthor = 64
	// MaxPluginDescription is the longest description of a plugin.
	MaxPluginDescription = 500
	// MaxPluginHosts is the most hosts that a plugin may ask to reach.
	MaxPluginHosts = 16
	// MaxPluginHostAddress is the longest address of a host.
	MaxPluginHostAddress = 255
	// MaxPluginHostReason is the longest reason for a host.
	MaxPluginHostReason = 200
)

var (
	// ErrPluginInfo is returned for a version, author, description or host
	// of a plugin that is too long or not valid.
	ErrPluginInfo = errors.New("invalid plugin information")
	// ErrPluginKind is returned for a change that the kind of the plugin
	// does not take, such as a new module for a plugin that runs as its own
	// program.
	ErrPluginKind = errors.New("the plugin is not of that kind")
)

// PluginHost is a host that a plugin inside DMRHub asks to reach, as
// host:port, and whether an admin allowed it.
type PluginHost struct {
	ID       uint   `gorm:"primaryKey"`
	PluginID uint   `gorm:"not null;index"`
	Address  string `gorm:"size:255;not null"`
	Reason   string `gorm:"size:200;not null;default:''"`
	Allowed  bool   `gorm:"not null;default:false"`
}

// TableName returns the table of the plugin hosts.
func (PluginHost) TableName() string {
	return "plugin_hosts"
}

// PluginModule is the WebAssembly module of an uploaded plugin, which every
// replica reads from the database.
type PluginModule struct {
	PluginID  uint   `gorm:"primaryKey;autoIncrement:false"`
	Hash      []byte `gorm:"size:32;not null"`
	Wasm      []byte `gorm:"not null"`
	CreatedAt time.Time
}

// TableName returns the table of the plugin modules.
func (PluginModule) TableName() string {
	return "plugin_modules"
}

// PluginInfo is who a plugin inside DMRHub says it is, and the hosts it asks
// to reach.
type PluginInfo struct {
	Name        string
	Version     string
	Author      string
	Description string
	Hosts       []PluginHost
}

// CheckPluginHost checks that address is a host and a TCP port.
func CheckPluginHost(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || len(address) > MaxPluginHostAddress {
		return fmt.Errorf("%w: host %q", ErrPluginInfo, address)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%w: port of %q", ErrPluginInfo, address)
	}
	return nil
}

// Check checks the lengths of the fields of i and its hosts.
func (i *PluginInfo) Check() error {
	switch {
	case len(i.Version) > MaxPluginVersion:
		return fmt.Errorf("%w: version", ErrPluginInfo)
	case len(i.Author) > MaxPluginAuthor:
		return fmt.Errorf("%w: author", ErrPluginInfo)
	case len(i.Description) > MaxPluginDescription:
		return fmt.Errorf("%w: description", ErrPluginInfo)
	case len(i.Hosts) > MaxPluginHosts:
		return fmt.Errorf("%w: at most %d hosts", ErrPluginInfo, MaxPluginHosts)
	}
	seen := map[string]bool{}
	for _, h := range i.Hosts {
		if err := CheckPluginHost(h.Address); err != nil {
			return err
		}
		if seen[h.Address] || len(h.Reason) > MaxPluginHostReason {
			return fmt.Errorf("%w: host %q", ErrPluginInfo, h.Address)
		}
		seen[h.Address] = true
	}
	return nil
}

func preloadPlugin(db *gorm.DB) *gorm.DB {
	return db.Preload("Ranges", func(q *gorm.DB) *gorm.DB { return q.Order("first_id") }).
		Preload("Hosts", func(q *gorm.DB) *gorm.DB { return q.Order("id") })
}

func replacePluginHosts(tx *gorm.DB, id uint, hosts []PluginHost, allowed func(string) bool) error {
	if err := tx.Where("plugin_id = ?", id).Delete(&PluginHost{}).Error; err != nil {
		return fmt.Errorf("could not replace the plugin hosts: %w", err)
	}
	if len(hosts) == 0 {
		return nil
	}
	rows := make([]PluginHost, len(hosts))
	for i, h := range hosts {
		rows[i] = PluginHost{PluginID: id, Address: h.Address, Reason: h.Reason, Allowed: allowed(h.Address)}
	}
	if err := tx.Create(&rows).Error; err != nil {
		return fmt.Errorf("could not store the plugin hosts: %w", err)
	}
	return nil
}

func storePluginModule(tx *gorm.DB, id uint, wasm []byte, now time.Time) ([]byte, error) {
	sum := sha256.Sum256(wasm)
	if err := tx.Where("plugin_id = ?", id).Delete(&PluginModule{}).Error; err != nil {
		return nil, fmt.Errorf("could not replace the plugin module: %w", err)
	}
	if err := tx.Create(&PluginModule{PluginID: id, Hash: sum[:], Wasm: wasm, CreatedAt: now.UTC()}).Error; err != nil {
		return nil, fmt.Errorf("could not store the plugin module: %w", err)
	}
	return sum[:], nil
}

// CreateUploadedPlugin adds a plugin that an admin uploaded as the
// WebAssembly module wasm, which says it is info, with ranges, and with
// the hosts in allowed among those it asks to reach allowed.
func CreateUploadedPlugin(db *gorm.DB, info PluginInfo, ranges []PluginRange, allowed []string, wasm []byte, now time.Time) (Plugin, error) {
	if err := info.Check(); err != nil {
		return Plugin{}, err
	}
	_, hash := newPluginToken()
	p := Plugin{
		Name: info.Name, TokenHash: hash, TokenCreatedAt: now.UTC(), Enabled: true, Kind: PluginUploaded,
		Version: info.Version, Author: info.Author, Description: info.Description,
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := checkPluginName(tx, 0, info.Name); err != nil {
			return err
		}
		if err := checkPluginRanges(tx, 0, ranges, true); err != nil {
			return err
		}
		if err := tx.Omit("Ranges", "Hosts").Create(&p).Error; err != nil {
			return fmt.Errorf("could not add the plugin: %w", err)
		}
		if err := replacePluginRanges(tx, p.ID, ranges); err != nil {
			return err
		}
		if err := replacePluginHosts(tx, p.ID, info.Hosts, func(a string) bool { return slices.Contains(allowed, a) }); err != nil {
			return err
		}
		sum, err := storePluginModule(tx, p.ID, wasm, now)
		if err != nil {
			return err
		}
		return tx.Model(&Plugin{}).Where("id = ?", p.ID).Update("module_hash", sum).Error
	})
	if err != nil {
		return Plugin{}, fmt.Errorf("could not add plugin %q: %w", info.Name, err)
	}
	return FindPlugin(db, p.ID)
}

// SetPluginModule replaces the module of the uploaded plugin id with wasm,
// a new version that says it is info. The hosts the admin allowed stay
// allowed, and the hosts the new version adds start out not allowed.
func SetPluginModule(db *gorm.DB, id uint, info PluginInfo, wasm []byte, now time.Time) (Plugin, error) {
	if err := info.Check(); err != nil {
		return Plugin{}, err
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		p, err := FindPlugin(tx, id)
		if err != nil {
			return err
		}
		if p.Kind != PluginUploaded {
			return ErrPluginKind
		}
		was := map[string]bool{}
		for _, h := range p.Hosts {
			was[h.Address] = h.Allowed
		}
		if err := replacePluginHosts(tx, id, info.Hosts, func(a string) bool { return was[a] }); err != nil {
			return err
		}
		sum, err := storePluginModule(tx, id, wasm, now)
		if err != nil {
			return err
		}
		return tx.Model(&Plugin{}).Where("id = ?", id).Select("ModuleHash", "Version", "Author", "Description").
			Updates(Plugin{ModuleHash: sum, Version: info.Version, Author: info.Author, Description: info.Description}).Error
	})
	if err != nil {
		return Plugin{}, fmt.Errorf("could not replace the module of plugin %d: %w", id, err)
	}
	return FindPlugin(db, id)
}

// SetAllowedPluginHosts allows the hosts in allowed among those that the
// plugin id asks to reach, and no others.
func SetAllowedPluginHosts(db *gorm.DB, id uint, allowed []string) (Plugin, error) {
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := FindPlugin(tx, id); err != nil {
			return err
		}
		if err := tx.Model(&PluginHost{}).Where("plugin_id = ?", id).Update("allowed", false).Error; err != nil {
			return fmt.Errorf("could not change the plugin hosts: %w", err)
		}
		if len(allowed) == 0 {
			return nil
		}
		if err := tx.Model(&PluginHost{}).Where("plugin_id = ? AND address IN ?", id, allowed).Update("allowed", true).Error; err != nil {
			return fmt.Errorf("could not change the plugin hosts: %w", err)
		}
		return nil
	})
	if err != nil {
		return Plugin{}, fmt.Errorf("could not allow the hosts of plugin %d: %w", id, err)
	}
	return FindPlugin(db, id)
}

// PluginModuleOf returns the hash and the WebAssembly module of the
// uploaded plugin id.
func PluginModuleOf(db *gorm.DB, id uint) ([]byte, []byte, error) {
	var found []PluginModule
	if err := db.Where("plugin_id = ?", id).Limit(1).Find(&found).Error; err != nil {
		return nil, nil, fmt.Errorf("could not read the plugin module: %w", err)
	}
	if len(found) == 0 {
		return nil, nil, ErrPluginNotFound
	}
	return found[0].Hash, found[0].Wasm, nil
}

// EnsureBuiltinPlugin returns the built-in plugin key, and adds it, named
// name with ranges, when there is none yet. When another plugin that is on
// has some of ranges, it waits for them, as WaitingPlugins says. Its
// version follows DMRHub's; its name, IDs and whether it is on stay as the
// admin set them.
func EnsureBuiltinPlugin(db *gorm.DB, key, name, version string, ranges []PluginRange, now time.Time) (Plugin, error) {
	var id uint
	err := db.Transaction(func(tx *gorm.DB) error {
		var found []Plugin
		if err := tx.Where("kind = ? AND builtin = ?", PluginBuiltin, key).Limit(1).Find(&found).Error; err != nil {
			return fmt.Errorf("could not find the built-in plugin: %w", err)
		}
		if len(found) > 0 {
			id = found[0].ID
			return tx.Model(&Plugin{}).Where("id = ?", id).Update("version", version).Error
		}
		if err := checkPluginName(tx, 0, name); err != nil {
			return err
		}
		if err := checkPluginRanges(tx, 0, ranges, false); err != nil {
			return err
		}
		_, hash := newPluginToken()
		p := Plugin{Name: name, TokenHash: hash, TokenCreatedAt: now.UTC(), Enabled: true, Kind: PluginBuiltin, Builtin: key, Version: version}
		if err := tx.Omit("Ranges", "Hosts").Create(&p).Error; err != nil {
			return fmt.Errorf("could not add the built-in plugin: %w", err)
		}
		id = p.ID
		return replacePluginRanges(tx, id, ranges)
	})
	if err != nil {
		return Plugin{}, fmt.Errorf("could not add the built-in plugin %s: %w", key, err)
	}
	return FindPlugin(db, id)
}

// NotePluginVersion records the version that the plugin id says it has.
func NotePluginVersion(db *gorm.DB, id uint, version string) error {
	if len(version) > MaxPluginVersion {
		version = version[:MaxPluginVersion]
	}
	if err := db.Model(&Plugin{}).Where("id = ?", id).Update("version", version).Error; err != nil {
		return fmt.Errorf("could not note the version of a plugin: %w", err)
	}
	return nil
}
