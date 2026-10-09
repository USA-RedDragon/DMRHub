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

package dmrdb

import (
	"encoding/json"
	"fmt"
)

// SetUint stores a JSON number token in dst. A null leaves dst unchanged,
// and any other type is reported as ErrDecodingDB so that format changes fail loudly.
func SetUint(dst *uint, t json.Token) error {
	switch v := t.(type) {
	case float64:
		*dst = uint(v) //nolint:gosec
		return nil
	case nil:
		return nil
	default:
		return fmt.Errorf("%w: expected a number, got %T", ErrDecodingDB, t)
	}
}

// SetString stores a JSON string token in dst. A null leaves dst unchanged,
// and any other type is reported as ErrDecodingDB so that format changes fail loudly.
func SetString(dst *string, t json.Token) error {
	switch v := t.(type) {
	case string:
		*dst = v
		return nil
	case nil:
		return nil
	default:
		return fmt.Errorf("%w: expected a string, got %T", ErrDecodingDB, t)
	}
}

// SkipValue consumes the rest of the value that starts with t, which only
// needs work when t opens an array or object.
func SkipValue(dec *json.Decoder, t json.Token) error {
	open, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if open != '[' && open != '{' {
		return ErrDecodingDB
	}
	depth := 1
	for depth > 0 {
		t, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrDecodingDB, err)
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '[', '{':
				depth++
			case ']', '}':
				depth--
			}
		}
	}
	return nil
}
