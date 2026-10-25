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

// Command echo is a test plugin that answers each burst to its IDs with the
// same burst, from its ID to the caller, a tenth of a second later.
package main

import (
	"context"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

func main() {
	plugin.MainWith(&pluginv1.Hello{
		Name: "Echo", Version: "1.0.0", Author: "N0CALL", Description: "Answers each burst.",
		Ids: []*pluginv1.IDRange{{First: 9100, Last: 9100}},
	}, func(_ context.Context, conn *plugin.Conn) error {
		for {
			m, err := conn.Recv()
			if err != nil {
				return err
			}
			b := m.GetBurst()
			if b == nil {
				continue
			}
			go func() {
				time.Sleep(100 * time.Millisecond)
				b.Source, b.Destination, b.GroupCall = b.GetDestination(), b.GetSource(), false
				_ = conn.Send(b)
			}()
		}
	})
}
