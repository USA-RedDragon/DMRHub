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
package smtp

import (
	"io"
	"net"
	"testing"

	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sinkSession struct {
	got chan string
}

func (s *sinkSession) Reset()                                    {}
func (s *sinkSession) Logout() error                             { return nil }
func (s *sinkSession) Mail(string, *smtp.MailOptions) error      { return nil }
func (s *sinkSession) Rcpt(to string, _ *smtp.RcptOptions) error { return nil }
func (s *sinkSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err //nolint:wrapcheck // the test server hands it back to the client
	}
	s.got <- string(b)
	return nil
}

// TestTLSNoneSendsToAServerWithoutSTARTTLS sends with the TLS mode none
// to a server that offers no STARTTLS, as a local relay does.
func TestTLSNoneSendsToAServerWithoutSTARTTLS(t *testing.T) {
	t.Parallel()
	got := make(chan string, 1)
	srv := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) {
		return &sinkSession{got: got}, nil
	}))
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = true
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr, ok := ln.Addr().(*net.TCPAddr)
	require.True(t, ok)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	cfg := &configPkg.Config{NetworkName: "DMRHub"}
	cfg.SMTP.Enabled = true
	cfg.SMTP.Host = "127.0.0.1"
	cfg.SMTP.Port = addr.Port
	cfg.SMTP.TLS = configPkg.SMTPTLSNone
	cfg.SMTP.AuthMethod = configPkg.SMTPAuthMethodNone
	cfg.SMTP.From = "hub@example.com"

	require.NoError(t, NewMailer(cfg).SendText("n0call@example.com", "Subject", "Body."))
	assert.Contains(t, <-got, "\r\n\r\nBody.\r\n")
}
