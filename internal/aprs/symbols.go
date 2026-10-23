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

// Symbol is an APRS symbol: its table and its code in that table.
type Symbol struct {
	Table byte
	Code  byte
}

const firstService = 5050

// SymbolNames returns the names of the symbols that an owner can pick, in
// the order of the location services 5050 to 5059.
func SymbolNames() []string {
	return []string{"house", "train", "tractor", "pickup", "van", "phone", "camp", "person", "bike", "car"}
}

// LookupSymbol returns the symbol called name.
func LookupSymbol(name string) (Symbol, bool) {
	code := map[string]byte{
		"house": '-', "train": '=', "tractor": 'F', "pickup": 'k', "van": 'v',
		"phone": '$', "camp": ';', "person": '[', "bike": 'b', "car": '>',
	}
	c, ok := code[name]
	return Symbol{Table: '/', Code: c}, ok
}

// ServiceSymbol returns the name of the symbol of the location service
// that a radio sent its position to, and false for any other ID.
func ServiceSymbol(service uint) (string, bool) {
	names := SymbolNames()
	if service < firstService || service >= firstService+uint(len(names)) {
		return "", false
	}
	return names[service-firstService], true
}

// MaxComment is the longest comment of a position report without a
// timestamp.
const MaxComment = 43

// ValidComment reports whether s fits in the comment of a position report:
// at most MaxComment printable ASCII characters, without | or ~.
func ValidComment(s string) bool {
	if len(s) > MaxComment {
		return false
	}
	for i := range len(s) {
		if c := s[i]; c < ' ' || c > '}' || c == '|' {
			return false
		}
	}
	return true
}

// ValidSSID reports whether s is empty or an SSID from 1 to 15.
func ValidSSID(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 2 || s[0] == '0' {
		return false
	}
	n := 0
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n >= 1 && n <= 15
}
