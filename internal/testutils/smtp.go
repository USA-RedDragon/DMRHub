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
package testutils

import (
	"io"
	"net"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/require"
)

type smtpSinkSession struct {
	got chan string
}

func (s *smtpSinkSession) Reset()                               {}
func (s *smtpSinkSession) Logout() error                        { return nil }
func (s *smtpSinkSession) Mail(string, *smtp.MailOptions) error { return nil }
func (s *smtpSinkSession) Rcpt(string, *smtp.RcptOptions) error { return nil }
func (s *smtpSinkSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err //nolint:wrapcheck // the test server hands it back to the client
	}
	s.got <- string(b)
	return nil
}

// StartSMTPSink starts an SMTP server on loopback without TLS or STARTTLS
// and points cfg at it. Each email it takes arrives on the returned
// channel, headers and body as sent.
func StartSMTPSink(t *testing.T, cfg *config.Config) <-chan string {
	t.Helper()
	got := make(chan string, 16)
	srv := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) {
		return &smtpSinkSession{got: got}, nil
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

	cfg.SMTP.Enabled = true
	cfg.SMTP.Host = "127.0.0.1"
	cfg.SMTP.Port = addr.Port
	cfg.SMTP.TLS = config.SMTPTLSNone
	cfg.SMTP.AuthMethod = config.SMTPAuthMethodNone
	cfg.SMTP.From = "hub@example.com"
	return got
}
