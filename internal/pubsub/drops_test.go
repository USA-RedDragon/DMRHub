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

package pubsub_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestADropIsLoggedAtPowersOfTwoPerTopic fills a feed and a subscription
// that nobody reads, and then publishes 8 and 5 messages more. Each drop
// is counted, but logged only at the 1st, 2nd, 4th, 8th... of its topic.
func TestADropIsLoggedAtPowersOfTwoPerTopic(t *testing.T) { //nolint:paralleltest // sets the default logger of the process
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ps.Close() })
	m := metrics.NewMetrics()
	feeds := testutil.ToFloat64(m.PubSubDroppedTotal.WithLabelValues("feed"))
	subs := testutil.ToFloat64(m.PubSubDroppedTotal.WithLabelValues("subscription"))

	f := ps.Feed()
	f.Add("drops-feed")
	for range 500 + 8 {
		require.NoError(t, ps.Publish("drops-feed", []byte{1}))
	}
	_ = ps.Subscribe("drops-sub")
	for range 100 + 5 {
		require.NoError(t, ps.Publish("drops-sub", []byte{1}))
	}

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	var feedLines, subLines []string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "topic=drops-feed"):
			feedLines = append(feedLines, l)
		case strings.Contains(l, "topic=drops-sub"):
			subLines = append(subLines, l)
		}
	}
	require.Len(t, feedLines, 4, "logs of the feed drops: %v", feedLines)
	assert.Contains(t, feedLines[3], "dropped=8")
	require.Len(t, subLines, 3, "logs of the subscription drops: %v", subLines)
	assert.Contains(t, subLines[2], "dropped=4")
	assert.InDelta(t, 8, testutil.ToFloat64(m.PubSubDroppedTotal.WithLabelValues("feed"))-feeds, 0)
	assert.InDelta(t, 5, testutil.ToFloat64(m.PubSubDroppedTotal.WithLabelValues("subscription"))-subs, 0)
}
