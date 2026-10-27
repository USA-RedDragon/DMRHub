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

package utils

import (
	"math"
	"testing"
)

func passwordEntropyBits(length, numbers, specials int) float64 {
	choose := func(n, k int) float64 {
		r := 1.0
		for i := 0; i < k; i++ {
			r = r * float64(n-i) / float64(i+1)
		}
		return r
	}
	letters := length - numbers - specials
	return math.Log2(choose(length, numbers)) + math.Log2(choose(length-numbers, specials)) +
		float64(letters)*math.Log2(float64(len(allowedChars))) +
		float64(numbers)*math.Log2(float64(len(allowedNumbers))) +
		float64(specials)*math.Log2(float64(len(allowedSpecial)))
}

func TestPasswordEntropy(t *testing.T) {
	t.Parallel()
	const minBits = 64
	if bits := passwordEntropyBits(peerPasswordLen, 1, 2); bits < 68 {
		t.Errorf("peer password has %.2f bits, want at least 68", bits)
	}
	if bits := passwordEntropyBits(repeaterPasswordLen, 4, 0); bits < minBits {
		t.Errorf("repeater password has %.2f bits, want at least %d", bits, minBits)
	}
}
