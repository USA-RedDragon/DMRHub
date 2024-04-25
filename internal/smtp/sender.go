// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

var (
	ErrEmailDisabled     = errors.New("email is disabled, but an email was attempted to be sent")
	ErrInvalidAuthMethod = errors.New("invalid SMTP auth method")
	ErrSendingEmail      = errors.New("error sending email")
)

func Send(toEmail string, subject string, body string) error {
	config := config.GetConfig()

	if !config.EnableEmail {
		logging.Errorf("Email is disabled, but an email was attempted to be sent")
		return ErrEmailDisabled
	}

	var auth sasl.Client
	switch config.SMTPAuthMethod {
	case "PLAIN":
		auth = sasl.NewPlainClient("", config.SMTPUsername, config.SMTPPassword)
	case "LOGIN":
		auth = sasl.NewLoginClient(config.SMTPUsername, config.SMTPPassword)
	default:
		logging.Errorf("Invalid SMTP auth method: %s", config.SMTPAuthMethod)
		return ErrInvalidAuthMethod
	}

	msg := strings.NewReader(fmt.Sprintf("From: %s <%s>\r\n", config.NetworkName, config.SMTPFrom) +
		fmt.Sprintf("To: %s\r\n", toEmail) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"Mime-Version: 1.0;\r\n" +
		"Content-Type: text/html; charset=\"ISO-8859-1\";\r\n" +
		"Content-Transfer-Encoding: 7bit;\r\n" +
		"\r\n<html><body>" +
		body +
		"\r\n</body></html>\r\n",
	)

	if config.SMTPImplicitTLS {
		err := smtp.SendMailTLS(
			config.SMTPHost+":"+fmt.Sprint(config.SMTPPort),
			auth,
			config.SMTPFrom,
			[]string{toEmail},
			msg)
		if err != nil {
			logging.Errorf("Error sending email: %v", err)
			return ErrSendingEmail
		}
	} else {
		err := smtp.SendMail(
			config.SMTPHost+":"+fmt.Sprint(config.SMTPPort),
			auth,
			config.SMTPFrom,
			[]string{toEmail},
			msg)
		if err != nil {
			logging.Errorf("Error sending email: %v", err)
			return ErrSendingEmail
		}
	}

	return nil
}
