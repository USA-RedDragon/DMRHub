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
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p) //nolint:wrapcheck
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestARefusedSpeechFormatFailsCleanly converts a call with an AMBE server
// that refuses the PKT_SPCHFMT field. Every frame goes out as silence for
// the reason ReasonSpeechFormat, and the hub logs why once. It does not run
// in parallel, as it takes over the default logger.
//
//nolint:paralleltest
func TestARefusedSpeechFormatFailsCleanly(t *testing.T) {
	var logs lockedBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	srv := testutils.StartFakeAMBEServer(t)
	srv.SetRefuseSpeechFormat(true)
	svc := newService(t, srv.Addr())
	in := call(t, voiceLC(t, enums.FIDStandardized), ambeFrames(6*layer2.VoiceSlotsPerBurst))
	st, err := svc.NewStream(transcode.CodecAMBE, transcode.CodecCodec2, codec2.Mode2400)
	require.NoError(t, err)
	t.Cleanup(st.Close)
	c2, err := codec2.New(&codec2.Config{Mode: codec2.Mode2400})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c2.Close() })
	s := c2.Silence()
	silence := [layer2.VoiceSlotsPerBurst][9]byte{s, s, s}
	for i := range in {
		r, ok := st.Convert(t.Context(), &in[i], deadline(st))
		require.True(t, ok)
		if !transcode.IsVoice(&in[i]) {
			continue
		}
		assert.Equal(t, layer2.VoiceSlotsPerBurst, r.Missed, "packet %d", i)
		assert.Equal(t, transcode.ReasonSpeechFormat, r.Reason, "packet %d", i)
		assert.Equal(t, silence, r.Burst.VoiceSlots(), "packet %d", i)
	}
	assert.Equal(t, 1, strings.Count(logs.String(), "level=ERROR"), logs.String())
	assert.Contains(t, logs.String(), "PKT_SPCHFMT")
	assert.Contains(t, logs.String(), srv.Addr())
	assert.Zero(t, srv.Decodes())
}
