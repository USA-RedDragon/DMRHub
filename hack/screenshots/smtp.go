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

package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
)

func startMailSink(ctx context.Context) (net.Listener, error) {
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", smtpPort))
	if err != nil {
		return nil, fmt.Errorf("could not start the mail sink: %w", err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go serveMail(c)
		}
	}()
	return l, nil
}

func serveMail(c net.Conn) {
	defer func() { _ = c.Close() }()
	r := bufio.NewReader(c)
	say := func(s string) bool {
		_, err := fmt.Fprintf(c, "%s\r\n", s)
		return err == nil
	}
	if !say("220 localhost ESMTP") {
		return
	}
	data := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if data {
			if line == "." {
				data = false
				say("250 OK")
			}
			continue
		}
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250 localhost")
		case strings.HasPrefix(cmd, "DATA"):
			data = true
			say("354 Go on")
		case strings.HasPrefix(cmd, "QUIT"):
			say("221 Bye")
			return
		default:
			say("250 OK")
		}
	}
}
