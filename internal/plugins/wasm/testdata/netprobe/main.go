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

// Command netprobe is a test plugin that dials the address in the payload
// of each burst to it, sends "ping", and answers with a burst whose payload
// holds the reply or the error.
package main

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

func probe(ctx context.Context, conn *plugin.Conn, address string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := conn.Dial(ctx, address)
	if err != nil {
		return "ERR " + err.Error()
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		return "ERR " + err.Error()
	}
	got, err := io.ReadAll(c)
	if err != nil {
		return "ERR " + err.Error()
	}
	return string(got)
}

func main() {
	plugin.MainWith(&pluginv1.Hello{
		Name: "Netprobe", Ids: []*pluginv1.IDRange{{First: 9200, Last: 9200}},
		Hosts: []*pluginv1.Host{{Address: "allowed.example.org:7", Reason: "Echoes."}},
	}, func(ctx context.Context, conn *plugin.Conn) error {
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
				out := probe(ctx, conn, string(bytes.TrimRight(b.GetPayload(), "\x00")))
				payload := make([]byte, 33)
				copy(payload, out)
				b.Source, b.Destination, b.GroupCall, b.Payload = b.GetDestination(), b.GetSource(), false, payload
				_ = conn.Send(b)
			}()
		}
	})
}
