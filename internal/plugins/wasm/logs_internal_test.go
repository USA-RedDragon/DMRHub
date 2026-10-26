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

package wasm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPluginSaysWhyItStops(t *testing.T) {
	t.Parallel()
	for want, lines := range map[string]string{
		"cannot reach the weather service": "time=2026-10-25T18:42:00Z level=INFO msg=up\ntime=2026-10-25T18:42:01Z level=ERROR msg=\"Plugin: stopped\" error=\"cannot reach the weather service\"\n",
		"quitting":                         "time=2026-10-25T18:42:01Z level=ERROR msg=\"Plugin: stopped\" error=quitting\n",
		"panic: no config":                 "panic: no config\n\ngoroutine 1 [running]:\nmain.main()\n\t/src/main.go:7 +0x2\n",
		"bad things":                       "bad things\n",
		"":                                 "",
	} {
		w := newLogWriter("test", 100)
		_, _ = w.Write([]byte(lines))
		assert.Equal(t, want, w.said(), lines)
	}
}
