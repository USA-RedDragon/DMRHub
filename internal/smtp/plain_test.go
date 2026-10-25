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
package smtp_test

import (
	"testing"

	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/smtp"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTLSNoneSendsToAServerWithoutSTARTTLS sends with the TLS mode none
// to a server that offers no STARTTLS, as a local relay does.
func TestTLSNoneSendsToAServerWithoutSTARTTLS(t *testing.T) {
	t.Parallel()
	cfg := &configPkg.Config{NetworkName: "DMRHub"}
	got := testutils.StartSMTPSink(t, cfg)

	require.NoError(t, smtp.NewMailer(cfg).SendText("n0call@example.com", "Subject", "Body."))
	assert.Contains(t, <-got, "\r\n\r\nBody.\r\n")
}
