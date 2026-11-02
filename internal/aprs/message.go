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

package aprs

import (
	"errors"
	"fmt"
	"strings"
)

// The limits of an APRS message, from the APRS 1.0.1 specification,
// chapter 14.
const (
	// MaxAddressee is the longest addressee of a message.
	MaxAddressee = 9
	// MaxMessageText is the longest text of a message.
	MaxMessageText = 67
	// MaxMessageNumber is the longest message number.
	MaxMessageNumber = 5
)

// ErrMessage is returned for a message that APRS cannot carry.
var ErrMessage = errors.New("invalid APRS message")

// Message is an APRS message from Source to Addressee.
type Message struct {
	// Source is the sender, as CALLSIGN or CALLSIGN-SSID.
	Source string `json:"source"`
	// Addressee is the station the message is for, at most 9 characters.
	Addressee string `json:"addressee"`
	// Text is at most 67 characters, with none of "|", "~" or "{".
	Text string `json:"text"`
	// Number is the message number of a message that wants an ack, 1 to
	// 5 letters or digits, or empty.
	Number string `json:"number,omitempty"`
}

// Check returns ErrMessage when m breaks a limit of APRS.
func (m *Message) Check() error {
	bad := func(what string) error { return fmt.Errorf("%w: %s", ErrMessage, what) }
	switch {
	case !validCallsign(m.Source):
		return bad("the source is not a callsign with an optional SSID")
	case m.Addressee == "" || len(m.Addressee) > MaxAddressee || !printable(m.Addressee) || strings.ContainsAny(m.Addressee, ": "):
		return bad("the addressee is not 1 to 9 characters")
	case len(m.Text) > MaxMessageText || !printable(m.Text) || strings.ContainsAny(m.Text, "|~{"):
		return bad("the text is over 67 characters or holds |, ~ or {")
	case len(m.Number) > MaxMessageNumber || !alphanumeric(m.Number):
		return bad("the message number is not 1 to 5 letters or digits")
	}
	return nil
}

// Info returns the information field of m: ":ADDRESSEE:text{number".
func (m *Message) Info() string {
	info := ":" + fmt.Sprintf("%-9s", m.Addressee) + ":" + m.Text
	if m.Number != "" {
		info += "{" + m.Number
	}
	return info
}

// MessageLine returns the line that sends m to APRS-IS from a client
// logged in as c: source>ToCall,TCPIP*:info. APRS-IS adds the q
// construct with the login.
func (c Config) MessageLine(m *Message) (string, error) {
	if err := m.Check(); err != nil {
		return "", err
	}
	return m.Source + ">" + ToCall + ",TCPIP*:" + m.Info(), nil
}

func printable(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func alphanumeric(s string) bool {
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
