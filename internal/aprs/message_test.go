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

package aprs_test

import (
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAMessageLineHasThePaddedAddresseeAndTheNumber(t *testing.T) {
	t.Parallel()
	c := aprs.Config{Login: "N0HUB"}
	line, err := c.MessageLine(&aprs.Message{Source: "KP4DJT-7", Addressee: "W1AW-9", Text: "Hello there", Number: "12"})
	require.NoError(t, err)
	assert.Equal(t, "KP4DJT-7>"+aprs.ToCall+",TCPIP*::W1AW-9   :Hello there{12", line)
	line, err = c.MessageLine(&aprs.Message{Source: "KP4DJT-7", Addressee: "W1AW-9", Text: "ack12"})
	require.NoError(t, err)
	assert.Equal(t, "KP4DJT-7>"+aprs.ToCall+",TCPIP*::W1AW-9   :ack12", line)
}

func TestAMessageThatAPRSCannotCarryIsRefused(t *testing.T) {
	t.Parallel()
	ok := aprs.Message{Source: "KP4DJT-7", Addressee: "W1AW-9", Text: strings.Repeat("x", aprs.MaxMessageText), Number: "12345"}
	require.NoError(t, ok.Check())
	for _, bad := range []aprs.Message{
		{Source: "kp4djt", Addressee: "W1AW", Text: "hi"},
		{Source: "KP4DJT-123", Addressee: "W1AW", Text: "hi"},
		{Source: "KP4DJT", Addressee: "", Text: "hi"},
		{Source: "KP4DJT", Addressee: "W1AW-12345", Text: "hi"},
		{Source: "KP4DJT", Addressee: "W1AW:", Text: "hi"},
		{Source: "KP4DJT", Addressee: "W1AW", Text: strings.Repeat("x", aprs.MaxMessageText+1)},
		{Source: "KP4DJT", Addressee: "W1AW", Text: "a{b"},
		{Source: "KP4DJT", Addressee: "W1AW", Text: "a|b"},
		{Source: "KP4DJT", Addressee: "W1AW", Text: "line\nbreak"},
		{Source: "KP4DJT", Addressee: "W1AW", Text: "hi", Number: "123456"},
		{Source: "KP4DJT", Addressee: "W1AW", Text: "hi", Number: "1-2"},
	} {
		require.ErrorIs(t, bad.Check(), aprs.ErrMessage, bad)
		_, err := aprs.Config{}.MessageLine(&bad)
		require.ErrorIs(t, err, aprs.ErrMessage)
	}
}
