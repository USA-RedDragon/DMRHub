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
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"gorm.io/gorm"
)

const (
	// ChangedTopic is the pubsub topic that says the plugins or their IDs
	// changed.
	ChangedTopic = "plugins:changed"
	inPrefix     = "plugins:in:"
	kickPrefix   = "plugins:kick:"
	sourcePrefix = "plugin:"
)

// InTopic is the pubsub topic of the bursts to the plugin id.
func InTopic(id uint) string {
	return inPrefix + strconv.FormatUint(uint64(id), 10)
}

func kickTopic(id uint) string {
	return kickPrefix + strconv.FormatUint(uint64(id), 10)
}

// SourceName is the name the hub routes the bursts of the plugin id under.
func SourceName(id uint) string {
	return sourcePrefix + strconv.FormatUint(uint64(id), 10)
}

// PluginOfSource returns the plugin whose bursts are routed under name, and
// false for any other source.
func PluginOfSource(name string) (uint, bool) {
	rest, ok := strings.CutPrefix(name, sourcePrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseUint(rest, 10, 64)
	return uint(id), err == nil
}

// PublishChanged tells every replica that the plugins or their IDs changed.
func PublishChanged(ps pubsub.PubSub) error {
	if err := ps.Publish(ChangedTopic, []byte{1}); err != nil {
		return fmt.Errorf("could not publish a plugin change: %w", err)
	}
	return nil
}

type owned struct {
	first, last uint
	plugin      uint
}

// Registry knows which plugin owns each DMR ID, among the plugins that are
// on, and hands the bursts to their IDs to the replica that holds the
// plugin's stream.
type Registry struct {
	db        *gorm.DB
	pubsub    pubsub.PubSub
	ranges    atomic.Pointer[[]owned]
	outside   atomic.Bool
	reloading sync.Mutex
}

// NewRegistry returns a Registry loaded from db, with the plugins that run
// inside DMRHub.
func NewRegistry(db *gorm.DB, ps pubsub.PubSub) (*Registry, error) {
	r := &Registry{db: db, pubsub: ps}
	empty := []owned{}
	r.ranges.Store(&empty)
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload reads the plugins and their IDs again.
func (r *Registry) Reload() error {
	r.reloading.Lock()
	defer r.reloading.Unlock()
	list, err := models.ListPlugins(r.db)
	if err != nil {
		return fmt.Errorf("could not load the plugins: %w", err)
	}
	waiting := models.WaitingPlugins(list)
	var out []owned
	for _, p := range list {
		if _, wait := waiting[p.ID]; wait || !p.Enabled || (p.Kind != models.PluginExternal && r.outside.Load()) {
			continue
		}
		for _, rg := range p.Ranges {
			out = append(out, owned{first: rg.First, last: rg.Last, plugin: p.ID})
		}
	}
	slices.SortFunc(out, func(a, b owned) int { return int(a.first) - int(b.first) })
	r.ranges.Store(&out)
	return nil
}

// SetInProcess says whether the plugins that run inside DMRHub run on this
// replica's hub. When they do not, nobody gets the calls to their IDs.
func (r *Registry) SetInProcess(on bool) error {
	r.outside.Store(!on)
	return r.Reload()
}

// Follow reloads the Registry each time PublishChanged says the plugins
// changed, until done is closed.
func (r *Registry) Follow(done <-chan struct{}) {
	sub := r.pubsub.Subscribe(ChangedTopic)
	go func() {
		defer func() { _ = sub.Close() }()
		for {
			select {
			case <-done:
				return
			case _, ok := <-sub.Channel():
				if !ok {
					return
				}
				if err := r.Reload(); err != nil {
					slog.Error("Plugins: cannot reload", "error", err)
				}
			}
		}
	}()
}

// Owner returns the plugin that owns the DMR ID id, and false when no plugin
// that is on owns it.
func (r *Registry) Owner(id uint) (uint, bool) {
	ranges := *r.ranges.Load()
	i, _ := slices.BinarySearchFunc(ranges, id, func(o owned, id uint) int {
		switch {
		case o.last < id:
			return -1
		case o.first > id:
			return 1
		}
		return 0
	})
	if i < len(ranges) && ranges[i].first <= id && id <= ranges[i].last {
		return ranges[i].plugin, true
	}
	return 0, false
}

// Deliver hands p to the plugin id, through the replica that holds its
// stream. A plugin that is not connected loses p. The error of a publish
// that did not reach pubsub is pubsub.ErrNotSent.
func (r *Registry) Deliver(id uint, p models.Packet) error {
	data, err := p.MarshalMsg(nil)
	if err != nil {
		return fmt.Errorf("encoding a burst for plugin %d: %w", id, err)
	}
	if err := r.pubsub.Publish(InTopic(id), data); err != nil {
		return fmt.Errorf("handing a burst to plugin %d: %w", id, err)
	}
	return nil
}
