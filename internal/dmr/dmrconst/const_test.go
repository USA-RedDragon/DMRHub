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

package dmrconst_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
)

func TestFrameTypeStringUnknown(t *testing.T) {
	t.Parallel()
	var frameType dmrconst.FrameType = 0xFF
	expected := "Unknown"
	if result := frameType.String(); result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestFrameTypeStringDataSync(t *testing.T) {
	t.Parallel()
	var frameType = dmrconst.FrameDataSync
	expected := "Data Sync"
	if result := frameType.String(); result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestFrameTypeStringVoiceSync(t *testing.T) {
	t.Parallel()
	var frameType = dmrconst.FrameVoiceSync
	expected := "Voice Sync"
	if result := frameType.String(); result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestFrameTypeStringVoice(t *testing.T) {
	t.Parallel()
	var frameType = dmrconst.FrameVoice
	expected := "Voice"
	if result := frameType.String(); result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func FuzzFrameTypeUnmarshalBinary(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte{0x01})
	f.Add([]byte{0x02})
	f.Add([]byte{0xFF})
	f.Add([]byte{}) // empty
	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		var ft dmrconst.FrameType
		_ = ft.UnmarshalBinary(data)
		// Also exercise MarshalBinaryTo if data was valid
		if len(data) >= 1 {
			out := make([]byte, 1)
			_ = ft.MarshalBinaryTo(out)
		}
		_ = ft.String()
	})
}
