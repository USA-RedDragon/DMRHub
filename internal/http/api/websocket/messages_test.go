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

package websocket_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	apiws "github.com/USA-RedDragon/DMRHub/internal/http/api/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/gin-contrib/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSession struct {
	sessions.Session
	values map[any]any
}

func (s fakeSession) Get(key any) any { return s.values[key] }

type recorder struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recorder) WriteMessage(m websocket.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, string(m.Data))
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.msgs...)
}

func newPubSub(t *testing.T) pubsub.PubSub {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	return ps
}

func TestMessagesWebsocketKeepsEachConnectionApart(t *testing.T) {
	t.Parallel()
	ps := newPubSub(t)
	h := apiws.CreateMessagesWebsocket(ps)
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/ws/messages", nil)
	require.NoError(t, err)
	var a, b recorder

	stopA := h.OnConnect(ctx, req, &a, fakeSession{values: map[any]any{"user_id": uint(1000001)}})
	stopB := h.OnConnect(ctx, req, &b, fakeSession{values: map[any]any{"user_id": uint(1000002)}})
	require.NotNil(t, stopA)
	require.NotNil(t, stopB)
	require.NoError(t, ps.Publish(messages.Topic(1000001), []byte(`{"to":"A"}`)))
	require.NoError(t, ps.Publish(messages.Topic(1000002), []byte(`{"to":"B"}`)))
	require.Eventually(t, func() bool { return len(a.got()) == 1 && len(b.got()) == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, []string{`{"to":"A"}`}, a.got())
	assert.Equal(t, []string{`{"to":"B"}`}, b.got())

	stopA()
	require.NoError(t, ps.Publish(messages.Topic(1000001), []byte(`{"to":"A2"}`)))
	require.NoError(t, ps.Publish(messages.Topic(1000002), []byte(`{"to":"B2"}`)))
	require.Eventually(t, func() bool { return len(b.got()) == 2 }, 2*time.Second, 10*time.Millisecond, "closing A leaves B open")
	time.Sleep(50 * time.Millisecond)
	assert.Len(t, a.got(), 1)
	stopB()
}

func TestMessagesWebsocketWithoutAUserSendsNothing(t *testing.T) {
	t.Parallel()
	ps := newPubSub(t)
	h := apiws.CreateMessagesWebsocket(ps)
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/ws/messages", nil)
	require.NoError(t, err)
	var r recorder
	assert.Nil(t, h.OnConnect(ctx, req, &r, fakeSession{values: map[any]any{}}))
	require.NoError(t, ps.Publish(messages.Topic(0), []byte(`{}`)))
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, r.got())
}
