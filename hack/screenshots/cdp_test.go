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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
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
	b.event(ctx, sent(t, "1", "main", "old", "http://hub/favicon.ico"))
	b.event(ctx, sent(t, "2", "child", "doc", "http://hub/tile.png"))
	b.event(ctx, sent(t, "3", "main", "new", "http://radio/callback"))
	b.event(ctx, cdpEvent(t, "Page.frameNavigated", map[string]any{"frame": map[string]string{"id": "main", "loaderId": "new", "url": "http://radio/callback"}}))
	assert.ElementsMatch(t, []string{"2", "3"}, pendingIDs(b))
	b.event(ctx, cdpEvent(t, "Page.frameDetached", map[string]string{"frameId": "child"}))
	assert.ElementsMatch(t, []string{"3"}, pendingIDs(b))
}

// TestACallThatChromiumNeverAnswersFails sends a command to a page that
// never answers. The call must fail with an error that names the command,
// so that the run stops instead of hanging.
func TestACallThatChromiumNeverAnswersFails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	b := &browser{conn: conn, waiting: map[int]chan cdpReply{}, pending: map[string]request{}, replyLimit: time.Millisecond}
	_, err = b.call(t.Context(), "Runtime.evaluate", map[string]any{"expression": "1"})
	require.ErrorIs(t, err, errTimeout)
	assert.Contains(t, err.Error(), "Runtime.evaluate: Chromium did not answer")
}
