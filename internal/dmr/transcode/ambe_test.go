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

package transcode_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialAMBEEncodesAndDecodesOnTheServer(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	c, err := transcode.DialAMBE(context.Background(), srv.Addr(), transcode.AttemptTimeout)
	require.NoError(t, err)
	pcm := make([]int16, c.FrameSamples())
	for i := range pcm {
		pcm[i] = int16(i * 37)
	}
	slot, err := c.Encode(pcm)
	require.NoError(t, err)
	assert.Equal(t, testutils.FakeAMBEEncode(pcm), slot)
	got, _, err := c.Decode(nil, slot)
	require.NoError(t, err)
	want := testutils.FakeAMBEDecode(slot)
	assert.Equal(t, want[:], got)
	require.NoError(t, c.Close())
	assert.Empty(t, srv.Errors())
}

func TestDialAMBEFailsWhenTheServerDoesNotAnswer(t *testing.T) {
	t.Parallel()
	srv := testutils.StartFakeAMBEServer(t)
	srv.SetAnswering(false)
	_, err := transcode.DialAMBE(context.Background(), srv.Addr(), time.Millisecond)
	require.Error(t, err)
}
