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
	"net"

	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"gorm.io/gorm"
)

// HeartbeatEvery is heartbeatEvery for the tests.
const HeartbeatEvery = heartbeatEvery

const TwinWait = twinWait

// ListenOn serves the plugins on lis, as Start does on its listener.
func ListenOn(lis net.Listener, secret []byte, db *gorm.DB, ps pubsub.PubSub, router Router, opts ...ServiceOption) (*Server, error) {
	registry, err := NewRegistry(db, ps)
	if err != nil {
		return nil, err
	}
	certs, err := NewCertSource(secret, "", "")
	if err != nil {
		return nil, err
	}
	return listening(lis, certs, registry, NewService(db, ps, registry, router, nil, opts...), func(*Registry) {}), nil
}
