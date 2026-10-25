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
	"crypto/sha1" //#nosec G505 -- HIBP range queries take SHA-1, not used for security here
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	gopwned "github.com/mavjs/goPwned"
)

// HIBPClientKey is the context key of the *http.Client that
// CheckPwnedPassword sends its HaveIBeenPwned queries with. Without one it
// uses a default client.
const HIBPClientKey = "HIBPClient"

// MsgPwnedPassword is the answer to a new password that HaveIBeenPwned
// lists.
const MsgPwnedPassword = "Password has been reported in a data breach. Please use another one"

var errBadPwnedCount = errors.New("bad count in a HaveIBeenPwned answer")

// CheckPwnedPassword asks HaveIBeenPwned whether password is in a data
// breach, when apiKey is set. It returns 0 and "" for a password that may
// be used, and otherwise the HTTP status and error to answer.
func CheckPwnedPassword(c *gin.Context, apiKey, password string) (int, string) {
	if apiKey == "" {
		return 0, ""
	}
	client, _ := c.Value(HIBPClientKey).(*http.Client)
	h := sha1.New() //#nosec G401 -- HIBP range queries take SHA-1, not used for security here
	h.Write([]byte(password))
	sum := fmt.Sprintf("%X", h.Sum(nil))
	karray, err := gopwned.NewClient(client, apiKey).GetPwnedPasswords(sum[0:5], false)
	if err != nil {
		if strings.HasPrefix(err.Error(), "Too many requests") {
			return http.StatusTooManyRequests, "Too many requests. Please try again in one minute"
		}
		slog.Error("Error getting pwned passwords", "error", err)
		return http.StatusInternalServerError, "Error getting pwned passwords"
	}
	count, err := pwnedCount(string(karray), sum[5:40])
	if err != nil {
		slog.Error("Error parsing pwned password count", "error", err)
		return http.StatusInternalServerError, "Error parsing pwned password count"
	}
	if count > 0 {
		return http.StatusBadRequest, MsgPwnedPassword
	}
	return 0, ""
}

// pwnedCount returns how often the hash suffix appears in a HaveIBeenPwned
// range answer. Lines without a count, such as a trailing empty line, are
// skipped.
func pwnedCount(answer, suffix string) (int64, error) {
	var result int64
	for line := range strings.SplitSeq(answer, "\r\n") {
		hash, n, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		count, err := strconv.ParseInt(strings.TrimSpace(n), 0, 32)
		if err != nil {
			return 0, fmt.Errorf("%w: %w", errBadPwnedCount, err)
		}
		if hash == suffix {
			result = count
		}
	}
	return result, nil
}
