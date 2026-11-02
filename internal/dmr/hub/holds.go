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
	"sync"
	"time"
)

// HoldStreamClaimWaits makes the waits of the hub for the answers of the
// KV store for the claims of streams end only when end is called, instead
// of after streamClaimWait.
func (h *Hub) HoldStreamClaimWaits() (end func()) {
	ch := make(chan time.Time)
	h.claimWaitEnd.Store(&ch)
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// HoldLastSeenWaits makes the waits of the hub for the records in the KV
// store of where radios were heard end only when end is called, instead
// of after lookup.Wait.
func (h *Hub) HoldLastSeenWaits() (end func()) {
	ch := make(chan struct{})
	h.lastSeen.HoldWaits(ch)
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// HoldLookupWaits makes the reads of the hub of the database, and of the
// owners of repeaters in the KV store, wait for their loads until end is
// called, instead of for at most lookup.Wait.
func (h *Hub) HoldLookupWaits() (end func()) {
	ch := make(chan struct{})
	h.talkgroupIDs.HoldWaits(ch)
	h.radioOwners.HoldWaits(ch)
	h.radioCalls.HoldWaits(ch)
	h.egressTable.HoldWaits(ch)
	h.tgRepeaters.HoldWaits(ch)
	h.repeaters.flights.HoldWaits(ch)
	h.owners.HoldWaits(ch)
	h.unlockGrants.HoldWaits(ch)
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}
