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
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"

	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

var (
	ErrEmailDisabled     = errors.New("email is disabled, but an email was attempted to be sent")
	ErrInvalidAuthMethod = errors.New("invalid SMTP auth method")
	ErrSendingEmail      = errors.New("error sending email")
	ErrHeaderLineBreak   = errors.New("an email header has a line break")
)

func SendToAdmins(config *configPkg.Config, db *gorm.DB, subject, body string) error {
	if !config.SMTP.Enabled {
		return nil
	}
	errGroup := errgroup.Group{}
	users, err := models.FindUserAdmins(db)
	if err != nil {
		return fmt.Errorf("failed to fetch admin users: %w", err)
	}
	for _, user := range users {
		if user.Email == "" {
			continue
		}
		errGroup.Go(func() error {
			return send(
				config,
				user.Email,
				subject,
				body,
			)
		})
	}
	if err := errGroup.Wait(); err != nil {
		return fmt.Errorf("failed to send email to admins: %w", err)
	}
	return nil
}

func send(config *configPkg.Config, toEmail string, subject string, body string) error {
	msg := fmt.Sprintf("From: %s <%s>\r\n", config.NetworkName, config.SMTP.From) +
		fmt.Sprintf("To: %s\r\n", toEmail) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"Mime-Version: 1.0;\r\n" +
		"Content-Type: text/html; charset=\"ISO-8859-1\";\r\n" +
		"Content-Transfer-Encoding: 7bit;\r\n" +
		"\r\n<html><body>" +
		html.EscapeString(body) +
		"\r\n</body></html>\r\n"
	return deliver(config, toEmail, msg)
}

// Mailer sends plain text email.
type Mailer interface {
	SendText(toEmail, subject, body string) error
}

// NewMailer returns the Mailer that sends with the SMTP settings of
// config.
func NewMailer(config *configPkg.Config) Mailer {
	return textMailer{config: config}
}

type textMailer struct {
	config *configPkg.Config
}

func (m textMailer) SendText(toEmail, subject, body string) error {
	msg, err := textMessage(m.config, toEmail, subject, body)
	if err != nil {
		return err
	}
	return deliver(m.config, toEmail, msg)
}

// textMessage returns the plain text email of body to toEmail, with CRLF
// line ends. A header value with a line break is refused, so that it
// cannot add headers.
func textMessage(config *configPkg.Config, toEmail, subject, body string) (string, error) {
	for _, v := range []string{config.NetworkName, config.SMTP.From, toEmail, subject} {
		if strings.ContainsAny(v, "\r\n") {
			return "", ErrHeaderLineBreak
		}
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	return fmt.Sprintf("From: %s <%s>\r\n", config.NetworkName, config.SMTP.From) +
		fmt.Sprintf("To: %s\r\n", toEmail) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		strings.Join(lines, "\r\n") + "\r\n", nil
}

func deliver(config *configPkg.Config, toEmail string, msg string) error {
	if !config.SMTP.Enabled {
		slog.Error("Email is disabled, but an email was attempted to be sent")
		return ErrEmailDisabled
	}

	var auth sasl.Client
	switch config.SMTP.AuthMethod {
	case configPkg.SMTPAuthMethodNone:
		auth = nil // No authentication
	case configPkg.SMTPAuthMethodPlain:
		auth = sasl.NewPlainClient("", config.SMTP.Username, config.SMTP.Password)
	case configPkg.SMTPAuthMethodLogin:
		auth = sasl.NewLoginClient(config.SMTP.Username, config.SMTP.Password)
	default:
		slog.Error("Invalid SMTP auth method", "method", config.SMTP.AuthMethod)
		return ErrInvalidAuthMethod
	}

	switch config.SMTP.TLS {
	case configPkg.SMTPTLSNone:
		if err := sendPlain(config.SMTP.Host+":"+fmt.Sprint(config.SMTP.Port), auth, config.SMTP.From, toEmail, msg); err != nil {
			slog.Error("Error sending email", "error", err)
			return ErrSendingEmail
		}
	case configPkg.SMTPTLSStartTLS:
		err := smtp.SendMail(
			config.SMTP.Host+":"+fmt.Sprint(config.SMTP.Port),
			auth,
			config.SMTP.From,
			[]string{toEmail},
			strings.NewReader(msg))
		if err != nil {
			slog.Error("Error sending email", "error", err)
			return ErrSendingEmail
		}
	case configPkg.SMTPTLSImplicit:
		err := smtp.SendMailTLS(
			config.SMTP.Host+":"+fmt.Sprint(config.SMTP.Port),
			auth,
			config.SMTP.From,
			[]string{toEmail},
			strings.NewReader(msg))
		if err != nil {
			slog.Error("Error sending email with TLS", "error", err)
			return ErrSendingEmail
		}
	}

	return nil
}

// sendPlain sends msg without TLS, for the TLS mode none. smtp.SendMail
// always asks for STARTTLS and fails on a server without it.
func sendPlain(addr string, auth sasl.Client, from, toEmail, msg string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}
	defer func() {
		_ = c.Close()
	}()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("could not authenticate: %w", err)
		}
	}
	if err := c.SendMail(from, []string{toEmail}, strings.NewReader(msg)); err != nil {
		return fmt.Errorf("could not send: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("could not quit: %w", err)
	}
	return nil
}
