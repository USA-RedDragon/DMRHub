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

package openbridge

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// feedPubSub hands each subscriber the messages that the test puts on feed.
type feedPubSub struct {
	pubsub.PubSub
	feed chan []byte
}

func (p feedPubSub) Subscribe(string) pubsub.Subscription { return feedSub(p) }

type feedSub feedPubSub

func (s feedSub) Close() error           { return nil }
func (s feedSub) Channel() <-chan []byte { return s.feed }

// TestAVersionOfAPeerBeforeItsChangeIsDropped has a replica that has not
// yet read a change of a peer note a BCVE of the peer, after another
// replica applied the change. The message and the stored version of the
// stale replica come after the change, so they must not move the link of
// the changed peer up on the other replica or on a replica that starts
// later. A version that the changed peer names must.
func TestAVersionOfAPeerBeforeItsChangeIsDropped(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	cfg.DMR.OpenBridge.Port = 0
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	store, err := kv.MakeKV(t.Context(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := t.Context()

	replica := func(feed chan []byte) *Server {
		s, err := MakeServer(&cfg, nil, database, feedPubSub{feed: feed}, store)
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Server.Close() })
		return &s
	}
	peerOf := func(s *Server) models.Peer {
		table, err := s.peerTable()
		require.NoError(t, err)
		require.Contains(t, table.byID, uint(7))
		return table.byID[7].Peer
	}
	published := func(s *Server) []byte {
		select {
		case m := <-s.egressOut:
			require.Equal(t, versionTopic, m.topic)
			return m.msg
		default:
			require.FailNow(t, "the replica published no version")
			return nil
		}
	}
	deliver := func(s *Server, feed chan []byte, msg []byte) {
		feed <- msg
		close(feed)
		s.wg.Add(1)
		s.consumeVersions()
	}

	require.NoError(t, database.Create(&models.Peer{ID: 7, Password: "secret", IP: "127.0.0.1", Port: 62035, Egress: true}).Error)
	aFeed := make(chan []byte, 1)
	a, stale := replica(aFeed), replica(nil)
	old := peerOf(stale)
	_ = peerOf(a)

	require.NoError(t, database.Model(&models.Peer{}).Where("id = ?", 7).Update("enhanced", false).Error)
	a.peers.Reload(struct{}{})
	a.forgetVersion(ctx, 7)
	changed := peerOf(a)
	require.True(t, changed.UpdatedAt.After(old.UpdatedAt), "the edit moved the UpdatedAt of the peer")

	stale.noteVersion(ctx, old, 5)
	deliver(a, aFeed, published(stale))
	assert.Equal(t, uint8(1), a.linkVersion(changed), "a version of the peer before its change moved the link up")
	late := replica(nil)
	late.loadVersions(ctx)
	assert.Equal(t, uint8(1), late.linkVersion(changed), "a replica that starts later took a version of the peer before its change")

	a.noteVersion(ctx, changed, 5)
	assert.Equal(t, uint8(5), a.linkVersion(changed), "the changed peer named version 5")
	later := replica(nil)
	later.loadVersions(ctx)
	assert.Equal(t, uint8(5), later.linkVersion(changed), "a replica that starts later takes the version of the changed peer")
}
