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

package apimodels_test

import (
	"encoding/json"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallResponseShowsTheCodec shows the codec of a call, its Codec2
// mode, the targets it was converted for and the audio that could not be.
func TestCallResponseShowsTheCodec(t *testing.T) {
	t.Parallel()
	call := &models.Call{ID: 7, Kind: models.CallKindVoice, Codec: models.CodecCodec2, Codec2Mode: 3200, SentAMBE: 4, SentCodec2: 1, MissedMS: 1200, TranscodeBusy: true}
	b, err := json.Marshal(apimodels.NewWSCallResponseFromCall(call))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "codec2", got["codec"])
	assert.InDelta(t, 3200, got["codec2_mode"], 0)
	assert.InDelta(t, 4, got["sent_ambe"], 0)
	assert.InDelta(t, 1, got["sent_codec2"], 0)
	assert.InDelta(t, 1200, got["missed_ms"], 0)
	assert.Equal(t, true, got["busy"])
}
