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

package calltracker_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
)

// TestTrackVoiceCodec records the codec of a voice call, its Codec2 mode,
// the audio that could not be converted, and each target that got the call
// converted, once, also from another replica and after the call ended.
func TestTrackVoiceCodec(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database := newReplicas(t)

		s := newSender(t, one, userA, tgID, true, 7101)
		head := s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), layer2.RawBurst{})
		one.VoiceCodec(head, models.CodecCodec2, 0)
		burst := s.send(dmrconst.FrameVoiceSync, 0, layer2.RawBurst{})
		one.VoiceCodec(burst, models.CodecCodec2, 3200)
		one.Missed(burst, 60)
		one.Converted(burst, models.CodecAMBE, "repeater:311860")
		one.Converted(burst, models.CodecAMBE, "repeater:311860")
		two.Converted(burst, models.CodecAMBE, "peer:3129201")
		one.Missed(burst, 20)
		one.Busy(burst)
		time.Sleep(150 * time.Millisecond)
		term := s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})

		c := waitDone(t, one, database, 1)[0]
		assert.Equal(t, models.CodecCodec2, c.Codec)
		assert.Equal(t, 3200, c.Codec2Mode)
		assert.Equal(t, 80, c.MissedMS)
		assert.Equal(t, 2, c.SentAMBE, "each target counts once, also from another replica")
		assert.Zero(t, c.SentCodec2)
		assert.True(t, c.TranscodeBusy)

		two.Converted(term, models.CodecAMBE, "repeater:311861")
		bubble.Eventually(t, func() bool {
			one.Flush()
			var got models.Call
			return database.First(&got, c.ID).Error == nil && got.SentAMBE == 3
		}, 5*time.Second, 20*time.Millisecond, "a target after the end of the call is saved")
	})
}
