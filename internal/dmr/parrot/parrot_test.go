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

package parrot_test

import (
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/parrot"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
)

func makeTestParrot(t *testing.T) (*parrot.Parrot, func()) {
	t.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	assert.NoError(t, err)
	p := parrot.NewParrot(kvStore)

	cleanup := func() {
		_ = kvStore.Close()
	}
	return p, cleanup
}

func TestNewParrot(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	assert.NotNil(t, p)
}

func TestParrotIsStartedReturnsFalseForNewStream(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	assert.False(t, p.IsStarted(context.Background(), 12345))
}

func TestParrotStartStream(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	ctx := context.Background()
	started := p.StartStream(ctx, 12345, 100)
	assert.True(t, started, "expected stream to start successfully")

	assert.True(t, p.IsStarted(ctx, 12345), "expected stream to be started")
}

func TestParrotStartStreamDuplicate(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	ctx := context.Background()
	started := p.StartStream(ctx, 12345, 100)
	assert.True(t, started)

	// Starting the same stream again should return false
	started = p.StartStream(ctx, 12345, 100)
	assert.False(t, started, "expected duplicate start to return false")
}

func TestParrotStopStream(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	ctx := context.Background()
	p.StartStream(ctx, 12345, 100)
	assert.True(t, p.IsStarted(ctx, 12345))

	p.StopStream(ctx, 12345)
	assert.False(t, p.IsStarted(ctx, 12345), "expected stream to be stopped")
}

func TestParrotRecordAndGetStream(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	ctx := context.Background()
	p.StartStream(ctx, 12345, 100)

	packet := models.Packet{
		Src:      200,
		Dst:      9990,
		Repeater: 100,
		StreamID: 12345,
		Slot:     false,
	}

	p.RecordPacket(ctx, 12345, packet)

	packets := p.GetStream(ctx, 12345)
	assert.NotNil(t, packets)
	assert.Len(t, packets, 1)

	// Verify src and dst are swapped
	assert.Equal(t, uint(9990), packets[0].Src)
	assert.Equal(t, uint(200), packets[0].Dst)
	// Verify repeater is set correctly
	assert.Equal(t, uint(100), packets[0].Repeater)
	// Verify it's marked as private call
	assert.False(t, packets[0].GroupCall)
}

func TestParrotRecordMultiplePackets(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	ctx := context.Background()
	p.StartStream(ctx, 12345, 100)

	for i := 0; i < 5; i++ {
		packet := models.Packet{
			Src:      200,
			Dst:      9990,
			Repeater: 100,
			StreamID: 12345,
			Slot:     false,
		}
		p.RecordPacket(ctx, 12345, packet)
	}

	packets := p.GetStream(ctx, 12345)
	assert.NotNil(t, packets)
	assert.Len(t, packets, 5)
}

func TestParrotGetStreamDrains(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	ctx := context.Background()
	p.StartStream(ctx, 12345, 100)

	packet := models.Packet{
		Src:      200,
		Dst:      9990,
		Repeater: 100,
		StreamID: 12345,
		Slot:     false,
	}
	p.RecordPacket(ctx, 12345, packet)

	// First get should have packets
	packets := p.GetStream(ctx, 12345)
	assert.Len(t, packets, 1)

	// Second get should be empty (drained)
	packets = p.GetStream(ctx, 12345)
	assert.Len(t, packets, 0)
}

func TestParrotStopStreamNonExistent(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	// Stopping a non-existent stream should not panic
	assert.NotPanics(t, func() {
		p.StopStream(context.Background(), 99999)
	})
}

func TestParrotMultipleStreams(t *testing.T) {
	t.Parallel()
	p, cleanup := makeTestParrot(t)
	defer cleanup()

	ctx := context.Background()
	p.StartStream(ctx, 11111, 100)
	p.StartStream(ctx, 22222, 200)

	assert.True(t, p.IsStarted(ctx, 11111))
	assert.True(t, p.IsStarted(ctx, 22222))

	p.StopStream(ctx, 11111)

	assert.False(t, p.IsStarted(ctx, 11111))
	assert.True(t, p.IsStarted(ctx, 22222))
}
