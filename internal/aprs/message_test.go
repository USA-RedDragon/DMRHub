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
	c := aprs.Config{Login: "N8CALL"}
	line, err := c.MessageLine(&aprs.Message{Source: "N1CALL-7", Addressee: "N2CALL-9", Text: "Hello there", Number: "12"})
	require.NoError(t, err)
	assert.Equal(t, "N1CALL-7>"+aprs.ToCall+",TCPIP*::N2CALL-9 :Hello there{12", line)
	line, err = c.MessageLine(&aprs.Message{Source: "N1CALL-7", Addressee: "N2CALL-9", Text: "ack12"})
	require.NoError(t, err)
	assert.Equal(t, "N1CALL-7>"+aprs.ToCall+",TCPIP*::N2CALL-9 :ack12", line)
}

func TestAMessageThatAPRSCannotCarryIsRefused(t *testing.T) {
	t.Parallel()
	ok := aprs.Message{Source: "N1CALL-7", Addressee: "N2CALL-9", Text: strings.Repeat("x", aprs.MaxMessageText), Number: "12345"}
	require.NoError(t, ok.Check())
	for _, bad := range []aprs.Message{
		{Source: "n1call", Addressee: "N2CALL", Text: "hi"},
		{Source: "N1CALL-123", Addressee: "N2CALL", Text: "hi"},
		{Source: "N1CALL", Addressee: "", Text: "hi"},
		{Source: "N1CALL", Addressee: "N2CALL-12345", Text: "hi"},
		{Source: "N1CALL", Addressee: "N2CALL:", Text: "hi"},
		{Source: "N1CALL", Addressee: "N2CALL", Text: strings.Repeat("x", aprs.MaxMessageText+1)},
		{Source: "N1CALL", Addressee: "N2CALL", Text: "a{b"},
		{Source: "N1CALL", Addressee: "N2CALL", Text: "a|b"},
		{Source: "N1CALL", Addressee: "N2CALL", Text: "line\nbreak"},
		{Source: "N1CALL", Addressee: "N2CALL", Text: "hi", Number: "123456"},
		{Source: "N1CALL", Addressee: "N2CALL", Text: "hi", Number: "1-2"},
	} {
		require.ErrorIs(t, bad.Check(), aprs.ErrMessage, bad)
		_, err := aprs.Config{}.MessageLine(&bad)
		require.ErrorIs(t, err, aprs.ErrMessage)
	}
}

func TestAMessageFromAPRSISIsRead(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]aprs.Message{
		"N2CALL-9>APDR16,TCPIP*,qAC,T2TEST::N1CALL-7 :Hello there{12":                  {Source: "N2CALL-9", Addressee: "N1CALL-7", Text: "Hello there", Number: "12"},
		"N2CALL-9>APDR16,TCPIP*,qAC,T2TEST::N1CALL-7 :See you {at} six{AB}7":           {Source: "N2CALL-9", Addressee: "N1CALL-7", Text: "See you {at} six", Number: "AB}7"},
		"N2CALL-9>APDR16,TCPIP*,qAC,T2TEST::N1CALL-7 :ack12":                           {Source: "N2CALL-9", Addressee: "N1CALL-7", Text: "ack12"},
		"N2CALL-9>APDR16,TCPIP*,qAC,T2TEST::N1CALL   :no number":                       {Source: "N2CALL-9", Addressee: "N1CALL", Text: "no number"},
		"N3CALL>APRS,TCPIP*,qAC,T2TEST:}N2CALL-9>APDR16,TCPIP,N3CALL*::N1CALL-7 :hi{3": {Source: "N2CALL-9", Addressee: "N1CALL-7", Text: "hi", Number: "3"},
	} {
		got, ok := aprs.ParseMessage(line)
		require.True(t, ok, line)
		assert.Equal(t, want, got, line)
	}
	for _, line := range []string{
		"# aprsc 2.1 3 Nov 2026 12:00:00 GMT T2TEST",
		"N2CALL-9>APDR16,TCPIP*,qAC,T2TEST:!3016.03N/09744.59W>",
		"N2CALL-9>APDR16,TCPIP*,qAC,T2TEST::SHORT:hi",
		"N2CALL-9>APDR16,TCPIP*,qAC,T2TEST::N1CALL-7 hi",
		"no header at all",
		"N3CALL>APRS:}}}}}}}}}",
	} {
		_, ok := aprs.ParseMessage(line)
		assert.False(t, ok, line)
	}
}
