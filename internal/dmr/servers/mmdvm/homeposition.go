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

package mmdvm

import (
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
)

// homePosition stores the position of an RPTG of the session of l, which
// DMRGateway sends once a minute from gpsd for a network with Location=1,
// as the position of the repeater.
func (s *Server) homePosition(l *link, o dmrdsession.Output) {
	if s.DB == nil || s.byID[o.ID] != l {
		return
	}
	k, err := dmrd.DecodeControl(o.Packet, l.dialect)
	if err != nil {
		slog.Info("Ignoring an RPTG that does not decode", "repeaterID", o.ID, "error", err)
		return
	}
	lat, lon, err := models.ParseHomePosition(k.Text)
	if err != nil {
		slog.Info("Ignoring an RPTG home position that does not parse", "repeaterID", o.ID, "text", string(k.Text))
		return
	}
	id := uint(o.ID)
	s.records.Do(id, func() {
		if err := models.SetRepeaterPosition(s.DB, id, lat, lon); err != nil {
			slog.Error("Error saving the home position of a repeater", "repeaterID", id, "error", err)
		}
	})
}
