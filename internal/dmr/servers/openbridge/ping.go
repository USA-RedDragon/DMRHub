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

package openbridge

import (
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/puzpuzpuz/xsync/v4"
)

const (
	// pingEvery is how often a replica records the last ping of a peer
	// that keeps sending.
	pingEvery = 30 * time.Second
	// pingQueueSize is how many last pings may wait for the database.
	pingQueueSize = 64
)

// peerPing is a time at which a peer sent a packet with a good signature.
type peerPing struct {
	peer uint
	at   time.Time
}

// notePing records that peer sent a packet with a good signature. It
// hands the time to writePings at most once per pingEvery for each peer,
// so that the packet path does not wait for the database.
func (s *Server) notePing(peer uint) {
	if s.pinged == nil {
		return
	}
	now := time.Now()
	if last, ok := s.pinged.Load(peer); ok && now.Sub(last) < pingEvery {
		return
	}
	due := false
	s.pinged.Compute(peer, func(last time.Time, loaded bool) (time.Time, xsync.ComputeOp) {
		if loaded && now.Sub(last) < pingEvery {
			return last, xsync.CancelOp
		}
		due = true
		return now, xsync.UpdateOp
	})
	if !due {
		return
	}
	select {
	case s.pings <- peerPing{peer: peer, at: now}:
	default:
		s.pinged.Delete(peer)
	}
}

// writePings stores the last pings that notePing hands it. It leaves the
// generation of the peer as it is.
func (s *Server) writePings() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case p := <-s.pings:
			if err := s.DB.Model(&models.Peer{}).Where("id = ?", p.peer).UpdateColumn("last_ping", p.at).Error; err != nil {
				slog.Error("Error saving the last ping of a peer", "peerID", p.peer, "error", err)
			}
		}
	}
}
