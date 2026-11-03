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

package transcode

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/dvsi"
)

// DialAMBE returns a codec that encodes and decodes AMBE+2 frames on the
// AMBE server at addr, a UDP host:port, without a reset of the server,
// which other calls may be using. Each
// request waits at most timeout for its answer. Close frees the
// connection.
func DialAMBE(ctx context.Context, addr string, timeout time.Duration) (vocoder.Codec, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the AMBE server %s: %w", addr, err)
	}
	c, err := dvsi.New(conn, &dvsi.Config{Timeout: timeout, NoReset: true})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("the AMBE server %s did not start a call: %w", addr, err)
	}
	return closer{Codec: c, conn: conn}, nil
}

type closer struct {
	*dvsi.Codec
	conn net.Conn
}

func (c closer) Close() error {
	err := c.Codec.Close()
	if cerr := c.conn.Close(); err == nil {
		err = cerr
	}
	return err
}
