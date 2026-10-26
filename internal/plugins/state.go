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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
)

const (
	statePrefix = "dmrhub:plugins:inside:state:"
	// stateKeep is how long a StopState stays after the last stop of its
	// plugin, as long as it counts the stops.
	stateKeep = time.Hour
)

// StopState is how a plugin inside DMRHub last stopped by itself, as the
// replica that runs it tells the others.
type StopState struct {
	// StoppedAt is when it stopped.
	StoppedAt time.Time `json:"stopped_at"`
	// RestartAt is when it starts again.
	RestartAt time.Time `json:"restart_at"`
	// Reason says why it stopped, as a sentence.
	Reason string `json:"reason"`
	// Stops are the times it stopped in the last hour.
	Stops []time.Time `json:"stops"`
}

func stateKey(id uint) string {
	return statePrefix + strconv.FormatUint(uint64(id), 10)
}

// StateOf returns the StopState of the plugin id, and false when it did not
// stop by itself in the last hour.
func StateOf(ctx context.Context, store kv.KV, id uint, now time.Time) (StopState, bool, error) {
	ok, err := store.Has(ctx, stateKey(id))
	if err != nil {
		return StopState{}, false, fmt.Errorf("could not read the state of a plugin: %w", err)
	}
	if !ok {
		return StopState{}, false, nil
	}
	b, err := store.Get(ctx, stateKey(id))
	if err != nil {
		return StopState{}, false, fmt.Errorf("could not read the state of a plugin: %w", err)
	}
	var s StopState
	if err := json.Unmarshal(b, &s); err != nil {
		return StopState{}, false, nil //nolint:nilerr // a state of another version counts as none
	}
	s.Stops = recent(s.Stops, now)
	return s, len(s.Stops) > 0, nil
}

func recent(stops []time.Time, now time.Time) []time.Time {
	out := stops[:0]
	for _, t := range stops {
		if now.Sub(t) < stateKeep {
			out = append(out, t)
		}
	}
	return out
}

// NoteStop records, for the other replicas, that the plugin id stopped by
// itself at, for reason, and starts again at restart.
func NoteStop(ctx context.Context, store kv.KV, id uint, at, restart time.Time, reason string) error {
	prev, _, err := StateOf(ctx, store, id, at)
	if err != nil {
		return err
	}
	s := StopState{StoppedAt: at, RestartAt: restart, Reason: reason, Stops: append(prev.Stops, at)}
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("could not encode the state of a plugin: %w", err)
	}
	if err := store.Set(ctx, stateKey(id), b); err != nil {
		return fmt.Errorf("could not store the state of a plugin: %w", err)
	}
	if err := store.Expire(ctx, stateKey(id), stateKeep); err != nil {
		return fmt.Errorf("could not store the state of a plugin: %w", err)
	}
	return nil
}
