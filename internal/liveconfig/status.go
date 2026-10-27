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

package liveconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/configulator/v2"
)

// Setting is how one setting stands on this replica and the others.
type Setting struct {
	// Editable is whether Setup changes the setting on this replica.
	Editable bool `json:"editable"`
	// Locked is whether the config file, the environment or a flag of this
	// replica sets it.
	Locked bool `json:"locked"`
	// StartOnly is whether the setting takes effect at start only, so that
	// it comes from the deployment alone.
	StartOnly bool `json:"start_only"`
	// Source is where the value that this replica runs with comes from.
	Source Source `json:"source"`
	// Saved is the value saved in Setup, if any.
	Saved json.RawMessage `json:"saved,omitempty"`
	// Default is the default value; none for a secret.
	Default json.RawMessage `json:"default,omitempty"`
	// NotInEffect is whether this replica could not put into effect the
	// value it should run with: that of the deployment when the setting is
	// locked, else the saved value, else the default. It then keeps its
	// previous value.
	NotInEffect bool `json:"not_in_effect"`
	// Disagree is whether the replicas run with different values. Replicas
	// then holds the value of each.
	Disagree bool           `json:"disagree"`
	Replicas []ReplicaValue `json:"replicas,omitempty"`
}

// ReplicaValue is the value of a setting on one replica.
type ReplicaValue struct {
	Replica string          `json:"replica"`
	Self    bool            `json:"self"`
	Value   json.RawMessage `json:"value"`
	Source  Source          `json:"source"`
}

// ReplicaStatus is the last report of one replica.
type ReplicaStatus struct {
	ID   string `json:"id"`
	Self bool   `json:"self"`
	// Revision is the save that the replica runs with, and Current
	// whether that is the latest one.
	Revision    uint64       `json:"revision"`
	Current     bool         `json:"current"`
	At          time.Time    `json:"at"`
	ApplyErrors []ApplyError `json:"apply_errors"`
	Invalid     []FieldError `json:"invalid"`
}

// Status is how the settings stand on the replicas.
type Status struct {
	// Revision is the latest save, 0 when none was ever made.
	Revision uint64             `json:"revision"`
	Settings map[string]Setting `json:"settings"`
	Replicas []ReplicaStatus    `json:"replicas"`
	// Disagree is whether the replicas run with different values of any
	// setting.
	Disagree bool `json:"disagree"`
}

// Status returns how the settings stand on this replica and the others.
func (l *Live) Status(ctx context.Context) (Status, error) {
	latest, err := LoadSaved(ctx, l.opts.DB)
	if err != nil {
		return Status{}, err
	}
	records := l.otherRecords(ctx)
	l.mu.Lock()
	self := l.report
	self.Replica = l.rep.ID
	saved := l.saved
	settings := make(map[string]Setting, len(l.keys))
	running := l.cur.Get()
	wanted := l.effective(saved)
	def := defaults()
	for _, k := range l.keys {
		_, locked := l.opts.Locks[k.Path]
		s := Setting{Editable: l.Editable(k), Locked: locked, StartOnly: !k.Setup, Source: l.source(k)}
		if raw, ok := saved.Values[k.Path]; ok && k.Setup {
			s.Saved = raw
		}
		s.Default = defaultOf(k, def)
		s.NotInEffect = !k.Equal(running, wanted)
		settings[k.Path] = s
	}
	l.mu.Unlock()

	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	all := append([]Report{self}, make([]Report, 0, len(ids))...)
	for _, id := range ids {
		all = append(all, records[id])
	}

	st := Status{Revision: latest.Revision, Settings: settings, Replicas: make([]ReplicaStatus, 0, len(all))}
	for i, r := range all {
		st.Replicas = append(st.Replicas, ReplicaStatus{
			ID: r.Replica, Self: i == 0, Revision: r.Revision, Current: r.Revision == latest.Revision,
			At: r.At, ApplyErrors: r.ApplyErrors, Invalid: r.Invalid,
		})
	}
	for _, k := range l.keys {
		if k.Secret || !disagree(all, k.Path) {
			continue
		}
		s := settings[k.Path]
		s.Disagree = true
		for i, r := range all {
			s.Replicas = append(s.Replicas, ReplicaValue{Replica: r.Replica, Self: i == 0, Value: r.Values[k.Path], Source: r.Sources[k.Path]})
		}
		settings[k.Path] = s
		st.Disagree = true
	}
	return st, nil
}

func disagree(all []Report, path string) bool {
	if len(all) < 2 {
		return false
	}
	first := all[0].Values[path]
	for _, r := range all[1:] {
		if !bytes.Equal(first, r.Values[path]) {
			return true
		}
	}
	return false
}

func defaults() *config.Config {
	d, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		return nil
	}
	return &d
}

func defaultOf(k Key, defaults *config.Config) json.RawMessage {
	if k.Secret || defaults == nil {
		return nil
	}
	v, err := k.Value(defaults)
	if err != nil {
		return nil
	}
	return v
}

// PendingStatus is how the settings stand while DMRHub cannot reach its
// database. Only the database settings can change then, and only when the
// deployment sets none of them and canStore, so that the setup wizard can
// keep them; see DatabaseState.
func PendingStatus(locks Locks, canStore bool) Status {
	editable := canStore && !DatabaseLocked(locks)
	st := Status{Settings: map[string]Setting{}, Replicas: []ReplicaStatus{}}
	def := defaults()
	for _, k := range Keys() {
		lock, locked := locks[k.Path]
		s := Setting{Locked: locked, StartOnly: !k.Setup, Source: SourceDefault, Default: defaultOf(k, def)}
		if locked {
			s.Source = lock.Source
		}
		s.Editable = editable && under(k.Path, "database")
		st.Settings[k.Path] = s
	}
	return st
}
