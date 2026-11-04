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

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cdpEvent(t *testing.T, method string, params any) cdpMessage {
	t.Helper()
	p, err := json.Marshal(params)
	require.NoError(t, err)
	return cdpMessage{Method: method, Params: p}
}

func sent(t *testing.T, id, frame, loader, url string) cdpMessage {
	t.Helper()
	return cdpEvent(t, "Network.requestWillBeSent", map[string]any{
		"requestId": id, "frameId": frame, "loaderId": loader, "type": "Other",
		"request": map[string]string{"url": url, "method": "GET"},
	})
}

func pendingIDs(b *browser) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.pending))
	for id := range b.pending {
		ids = append(ids, id)
	}
	return ids
}

// TestANewDocumentEndsTheRequestsOfTheOldOne has a frame request a
// favicon, then commit a new document. Chromium does not always report the
// end of such a request, which then kept the page loading until the shot
// timed out. A request of the new document or of another frame stays.
func TestANewDocumentEndsTheRequestsOfTheOldOne(t *testing.T) {
	t.Parallel()
	b := &browser{pending: map[string]request{}}
	ctx := t.Context()
	b.event(ctx, sent(t, "1", "main", "old", "http://127.0.0.1:3955/favicon.ico"))
	b.event(ctx, sent(t, "2", "child", "doc", "http://127.0.0.1:3955/tile.png"))
	b.event(ctx, sent(t, "3", "main", "new", "http://127.0.0.1:4180/callback"))
	b.event(ctx, cdpEvent(t, "Page.frameNavigated", map[string]any{"frame": map[string]string{"id": "main", "loaderId": "new", "url": "http://127.0.0.1:4180/callback"}}))
	assert.ElementsMatch(t, []string{"2", "3"}, pendingIDs(b))
	b.event(ctx, cdpEvent(t, "Page.frameDetached", map[string]string{"frameId": "child"}))
	assert.ElementsMatch(t, []string{"3"}, pendingIDs(b))
}
