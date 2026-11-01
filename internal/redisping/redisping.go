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

// Package redisping reaches Redis at the startup of DMRHub.
package redisping

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/consts"
)

const (
	firstWait = 100 * time.Millisecond
	maxWait   = 2 * time.Second
)

// Until calls ping until it succeeds, waiting 100 ms after the first
// failure and twice as long after each next one, up to 2 s. It returns the
// error of ping at once when the error is permanent, such as a wrong
// password or a host that does not exist. It returns the last error once
// consts.RedisConnectWait has passed, and joins the error of ctx when ctx
// ends first.
func Until(ctx context.Context, ping func(context.Context) error) error {
	deadline := time.Now().Add(consts.RedisConnectWait)
	wait := firstWait
	for {
		err := ping(ctx)
		if err == nil {
			return nil
		}
		left := time.Until(deadline)
		if !transient(err) || left <= 0 {
			return err
		}
		timer := time.NewTimer(min(wait, left))
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
		wait = min(2*wait, maxWait)
	}
}

func transient(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTimeout || dnsErr.IsTemporary
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, e := range []error{context.DeadlineExceeded, syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE, io.EOF, io.ErrUnexpectedEOF} {
		if errors.Is(err, e) {
			return true
		}
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "LOADING ") || strings.HasPrefix(msg, "BUSY ") || strings.HasPrefix(msg, "TRYAGAIN ")
}
