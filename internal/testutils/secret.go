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

package testutils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

// Secret returns a password of the test t: the same at each call with t,
// and another one for any other test, also for another run of the same
// test. An OpenBridge client of t with it takes nothing that the servers
// of another test sign.
func Secret(t testing.TB) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%p", t.Name(), t))
	return hex.EncodeToString(sum[:8])
}
