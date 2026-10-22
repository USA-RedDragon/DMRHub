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
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
)

// RecordTalkerAlias records the talker alias block a that the repeater
// with the ID repeater sent to the server named sourceName.
func (h *Hub) RecordTalkerAlias(a dmrd.TalkerAlias, sourceName string, repeater uint) {
	h.callTracker.TalkerAlias(a, sourceName, repeater)
}

// RecordPosition records the GPS position p that the repeater with the ID
// repeater sent to the server named sourceName.
func (h *Hub) RecordPosition(p dmrd.Position, sourceName string, repeater uint) {
	h.callTracker.Position(p, sourceName, repeater)
}
