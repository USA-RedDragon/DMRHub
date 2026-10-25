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
	"testing"

	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextMessageIsPlainTextWithCRLF(t *testing.T) {
	t.Parallel()
	cfg := &configPkg.Config{NetworkName: "DMRHub"}
	cfg.SMTP.From = "hub@example.com"

	msg, err := textMessage(cfg, "n0call@example.com", "Choose a new password", "Line one.\n\nhttps://hub.example.com/a#b <c>")
	require.NoError(t, err)
	assert.Equal(t, "From: DMRHub <hub@example.com>\r\n"+
		"To: n0call@example.com\r\n"+
		"Subject: Choose a new password\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n"+
		"Content-Transfer-Encoding: 8bit\r\n"+
		"\r\n"+
		"Line one.\r\n\r\nhttps://hub.example.com/a#b <c>\r\n", msg)

	_, err = textMessage(cfg, "n0call@example.com\r\nBcc: x@example.com", "s", "b")
	assert.ErrorIs(t, err, ErrHeaderLineBreak)
}
