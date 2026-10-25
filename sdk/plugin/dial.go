// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build !wasip1

package plugin

import (
	"context"
	"fmt"
	"net"
)

// Dial connects to address, as host:port, over TCP. A plugin inside
// DMRHub reaches only the hosts that an admin allowed on its page, through
// the hub; a plugin that runs as its own program reaches any host.
func (c *Conn) Dial(ctx context.Context, address string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", address, err)
	}
	return conn, nil
}
