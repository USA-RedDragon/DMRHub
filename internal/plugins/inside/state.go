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

package inside

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
)

// reasonOf says why a plugin stopped with err, after it wrote said on its
// stderr, within limits l.
func reasonOf(err error, said string, l wasm.Limits) string {
	switch {
	case errors.Is(err, wasm.ErrMemory):
		return fmt.Sprintf("It used more memory than a plugin may (%d MiB).", uint64(l.MemoryPages)*64/1024)
	case errors.Is(err, wasm.ErrCPU):
		return fmt.Sprintf("It used more than %s for %s.", coreShare(l.CPUShare), seconds(l.CPUWindow))
	case errors.Is(err, wasm.ErrNoAnswer):
		return fmt.Sprintf("It did not answer DMRHub for %s.", seconds(l.AnswerWithin))
	}
	var ended *wasm.EndedError
	msg := said
	switch {
	case errors.As(err, &ended):
		msg = ended.Message()
	case msg == "" && err != nil:
		msg = err.Error()
	case msg == "":
		msg = "it ended"
	}
	msg = strings.TrimSpace(msg)
	if !strings.HasSuffix(msg, ".") && !strings.HasSuffix(msg, "!") && !strings.HasSuffix(msg, "?") {
		msg += "."
	}
	return "It crashed: " + msg
}

func coreShare(share float64) string {
	switch {
	case share == 0.5: //nolint:mnd // half a core
		return "half a CPU core"
	case share == 1:
		return "a CPU core"
	case share > 1:
		return strconv.FormatFloat(share, 'f', -1, 64) + " CPU cores"
	}
	return strconv.Itoa(int(math.Round(share*100))) + "% of a CPU core" //nolint:mnd // a percentage
}

func seconds(d time.Duration) string {
	return strconv.Itoa(int(d.Round(time.Second)/time.Second)) + " s"
}
