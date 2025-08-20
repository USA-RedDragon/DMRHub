// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"log/slog"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

var (
	ErrEmailDisabled     = errors.New("email is disabled, but an email was attempted to be sent")
	ErrInvalidAuthMethod = errors.New("invalid SMTP auth method")
	ErrSendingEmail      = errors.New("error sending email")
)

func SendToAdmins(config *config.Config, db *gorm.DB, subject, body string) error {
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
	return errGroup.Wait()
}

func send(config *configPkg.Config, toEmail string, subject string, body string) error {
	if !config.SMTP.Enabled {
		logging.Errorf("Email is disabled, but an email was attempted to be sent")
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

	msg := strings.NewReader(fmt.Sprintf("From: %s <%s>\r\n", config.NetworkName, config.SMTP.From) +
		fmt.Sprintf("To: %s\r\n", toEmail) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"Mime-Version: 1.0;\r\n" +
		"Content-Type: text/html; charset=\"ISO-8859-1\";\r\n" +
		"Content-Transfer-Encoding: 7bit;\r\n" +
		"\r\n<html><body>" +
		body +
		"\r\n</body></html>\r\n",
	)

	switch config.SMTP.TLS {
	case configPkg.SMTPTLSNone, configPkg.SMTPTLSStartTLS:
		err := smtp.SendMail(
			config.SMTP.Host+":"+fmt.Sprint(config.SMTP.Port),
			auth,
			config.SMTP.From,
			[]string{toEmail},
			msg)
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
			msg)
		if err != nil {
			slog.Error("Error sending email with TLS", "error", err)
			return ErrSendingEmail
		}
	}

	return nil
}
