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
	"context"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPublishToAStalledRedisIsSentOnce pauses the Redis container while a
// message is published. Redis takes the PUBLISH but runs it only after it
// resumes, so the publish times out. The message must then reach a
// subscriber at most once: a PUBLISH that the client sends again on
// another connection after the timeout also runs, and delivers the message
// twice.
func TestAPublishToAStalledRedisIsSentOnce(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	var c testutils.Containers
	testutils.PostgresRedisBackendInto(&c).Setup(t, &cfg)
	pub, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pub.Close() })
	sub, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	s := sub.Subscribe("stall")
	t.Cleanup(func() { _ = s.Close() })

	const warm = 16
	var wg sync.WaitGroup
	for range warm {
		wg.Go(func() { assert.NoError(t, pub.Publish("stall", []byte("warm"))) })
	}
	wg.Wait()
	for range warm {
		select {
		case <-s.Channel():
		case <-time.After(5 * time.Second):
			t.Fatal("a warm-up message did not arrive")
		}
	}

	docker := func(args ...string) {
		out, err := exec.Command("docker", args...).CombinedOutput() //nolint:gosec,noctx
		require.NoError(t, err, string(out))
	}
	docker("pause", c.Redis)
	t.Cleanup(func() { _ = exec.Command("docker", "unpause", c.Redis).Run() }) //nolint:gosec,noctx
	start := time.Now()
	err = pub.Publish("stall", []byte("once"))
	took := time.Since(start)
	docker("unpause", c.Redis)
	require.Error(t, err)

	got := 0
	deadline := time.After(3 * time.Second)
	for done := false; !done; {
		select {
		case m := <-s.Channel():
			if string(m) == "once" {
				got++
			}
		case <-deadline:
			done = true
		}
	}
	t.Logf("the publish to a stalled Redis returned %v after %v; the subscriber got the message %d times", err, took, got)
	assert.LessOrEqual(t, got, 1, "the message arrived more than once")
	assert.Less(t, took, 2*time.Second, "the publish waited for more than one Redis timeout")
}
