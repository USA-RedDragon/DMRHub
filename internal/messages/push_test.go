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

package messages_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/data"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pushRig(t *testing.T) (*messages.Store, pubsub.PubSub) {
	t.Helper()
	database := openDB(t)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
	require.NoError(t, err)
	store := messages.NewStore(database)
	store.SetNotify(messages.NewPusher(database, ps).Push)
	t.Cleanup(func() {
		store.Stop()
		_ = ps.Close()
	})
	var a models.User
	require.NoError(t, database.First(&a, userA).Error)
	require.NoError(t, models.SetSMSStorage(database, &a, true, time.Now().Add(-time.Minute)))
	return store, ps
}

func frames(t *testing.T, sub pubsub.Subscription, wait time.Duration) []apimodels.MessagePush {
	t.Helper()
	var out []apimodels.MessagePush
	deadline := time.After(wait)
	for {
		select {
		case b := <-sub.Channel():
			var f apimodels.MessagePush
			require.NoError(t, json.Unmarshal(b, &f))
			out = append(out, f)
		case <-deadline:
			return out
		}
	}
}

func TestPushGoesOnlyToOwnersWhoSeeTheMessage(t *testing.T) {
	t.Parallel()
	store, ps := pushRig(t)
	subA := ps.Subscribe(messages.Topic(userA))
	subB := ps.Subscribe(messages.Topic(userB))
	defer func() { _ = subA.Close(); _ = subB.Close() }()

	now := time.Now()
	h, payload := confirmed(t, "hello A", 1)
	store.DataPacket(&models.Call{ID: 1}, delivered(h, payload, false), now)
	store.Flush()

	got := frames(t, subA, 300*time.Millisecond)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"inbox"}, got[0].Boxes)
	require.NotNil(t, got[0].Message.Text)
	assert.Equal(t, "hello A", *got[0].Message.Text)
	assert.Equal(t, userB, got[0].Message.SourceID)
	assert.Empty(t, frames(t, subB, 100*time.Millisecond), "B has SMS Storage off")

	store.DataResponse(responseRow(2), responseEvent(packet.ResponseACK), now.Add(200*time.Millisecond))
	store.Flush()
	got = frames(t, subA, 300*time.Millisecond)
	require.Len(t, got, 1, "the ACK changes the message")
	assert.Equal(t, "ack", got[0].Message.Response)
}

func TestPushSkipsMessagesFromBeforeTheOptIn(t *testing.T) {
	t.Parallel()
	store, ps := pushRig(t)
	subA := ps.Subscribe(messages.Topic(userA))
	defer func() { _ = subA.Close() }()

	h, payload := confirmed(t, "old", 1)
	store.DataPacket(&models.Call{ID: 1}, delivered(h, payload, false), time.Now().Add(-time.Hour))
	store.Flush()
	assert.Empty(t, frames(t, subA, 200*time.Millisecond))
}

func TestPushPutsAGroupMessageInTheOutbox(t *testing.T) {
	t.Parallel()
	store, ps := pushRig(t)
	subA := ps.Subscribe(messages.Topic(userA))
	defer func() { _ = subA.Close() }()

	var h sms.Header
	payload, err := sms.AppendPacket(nil, &sms.Text{Format: sms.FormatIP, Group: true, Source: uint32(userA), Destination: uint32(tgID), Body: []byte("CQ")}, &h)
	require.NoError(t, err)
	store.DataPacket(&models.Call{ID: 1}, &data.Event{Kind: data.EventDelivered, Call: data.Call{Group: true, Source: uint32(userA), Target: uint32(tgID)}, Message: data.Message{Header: h.Data, Payload: payload}}, time.Now())
	store.Flush()
	got := frames(t, subA, 300*time.Millisecond)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"outbox"}, got[0].Boxes)
	assert.True(t, got[0].Message.Group)
}
