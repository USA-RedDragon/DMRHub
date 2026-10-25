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

package transcode

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/dvsi"
)

// Budget is the time a burst may take to convert, one burst period. A
// frame that the AMBE server does not answer within the budget of its
// burst goes out as silence; nothing waits for a later answer.
const Budget = 60 * time.Millisecond

// AttemptTimeout is how long one request to the AMBE server waits for
// its answer. A failed request is tried again when the budget of the
// burst leaves time for it; a try after a timeout takes up to two, so one
// lost answer and its retry fit in the Budget.
const AttemptTimeout = 18 * time.Millisecond

// FrameDuration is the audio in one vocoder frame.
const FrameDuration = 20 * time.Millisecond

// The reasons for audio that was not converted.
const (
	// ReasonTimeout is an AMBE server that did not answer in time.
	ReasonTimeout = "timeout"
	// ReasonUnreachable is an AMBE server that refused the request or
	// could not be reached.
	ReasonUnreachable = "unreachable"
	// ReasonError is an AMBE server that answered with an error.
	ReasonError = "error"
	// ReasonLate is a burst whose budget was over before it was converted.
	ReasonLate = "late"
	// ReasonCodec2 is a Codec2 frame that libcodec2 failed on.
	ReasonCodec2 = "codec2"
	// ReasonBusy is a call that found every AMBE server busy, so that it
	// is not converted at all.
	ReasonBusy = "busy"
)

// Result is a burst converted by a Stream.
type Result struct {
	// Burst is the burst in the codec of the copy.
	Burst layer2.RawBurst
	// Missed is the number of vocoder frames that went out as silence.
	Missed int
	// Reason is why the first missed frame was missed.
	Reason string
	// Busy reports a call that found every AMBE server busy. It has no
	// copy; Burst is empty and Missed counts its frames.
	Busy bool
}

// Stream converts the bursts of one call from one codec to the other. It
// is not safe for concurrent use.
type Stream struct {
	servers []*server
	srv     *server
	lease   *lease
	took    bool
	from    Codec
	to      Codec
	c2      *codec2.Codec
	conn    net.Conn
	ambe    *dvsi.Codec
	emb     embeddedLC
	failing bool
	stale   bool
	budget  time.Duration
	attempt time.Duration
	pcm     []int16
}

// Budget returns the time each burst of the call may take to convert.
func (s *Stream) Budget() time.Duration { return s.budget }

// From returns the codec of the call.
func (s *Stream) From() Codec { return s.from }

// To returns the codec of the copy.
func (s *Stream) To() Codec { return s.to }

// Close frees the codecs and the connection to the AMBE server, and gives
// the server back.
func (s *Stream) Close() {
	s.CloseContext(context.Background())
}

// CloseContext is Close with the context of the KV requests that give the
// server back.
func (s *Stream) CloseContext(ctx context.Context) {
	s.closeAMBE()
	if s.lease != nil {
		s.lease.release(ctx)
		s.lease = nil
	}
	if s.c2 != nil {
		_ = s.c2.Close()
		s.c2 = nil
	}
}

func (s *Stream) closeAMBE() {
	if s.ambe != nil {
		_ = s.ambe.Close()
		s.ambe = nil
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// Convert returns the burst of p in the codec of the copy: a Voice LC
// Header or Terminator with LC with the FID of that codec, or a voice
// burst with its three frames converted and its embedded LC rewritten. A
// frame that cannot be converted by deadline, or before ctx is done,
// becomes silence. ok is false for any other burst, which has no copy.
func (s *Stream) Convert(ctx context.Context, p *models.Packet, deadline time.Time) (Result, bool) {
	raw := layer2.RawBurst(p.DMRData)
	if dt, isData := DataTypeOf(p); isData && dt != enums.DataTypeVoiceLCHeader && dt != enums.DataTypeTerminatorWithLC {
		return Result{}, false
	} else if !isData && !IsVoice(p) {
		return Result{}, false
	}
	if !s.hold(ctx) {
		res := Result{Busy: true, Reason: ReasonBusy}
		if IsVoice(p) {
			res.Missed = layer2.VoiceSlotsPerBurst
		}
		return res, true
	}
	if dt, isData := DataTypeOf(p); isData {
		pl, ok := raw.MMDVMHostPayload(dt)
		if !ok {
			return Result{}, false
		}
		if !s.emb.have {
			s.emb.setLC(pdu.LCBytes(pl[:9]))
		}
		out, ok := withFID(raw, dt, FIDOf(s.to))
		if !ok {
			return Result{}, false
		}
		return Result{Burst: out}, true
	}
	res := Result{}
	slots := raw.VoiceSlots()
	var out [layer2.VoiceSlotsPerBurst][9]byte
	skip := false
	for k := range slots {
		var reason string
		out[k], reason = s.frame(ctx, slots[k], deadline, skip)
		if reason != "" {
			if res.Missed == 0 {
				res.Reason = reason
			}
			res.Missed++
			skip = skip || reason != ReasonCodec2
		}
	}
	raw.SetVoiceSlots(out)
	s.emb.rewrite(&raw, p.DTypeOrVSeq)
	res.Burst = raw
	return res, true
}

// hold gives the call its AMBE server at its first burst: the first one,
// in order, that has room. It renews the hold at later bursts, and reports
// false for a call that found every server busy.
func (s *Stream) hold(ctx context.Context) bool {
	if s.took {
		if s.lease != nil {
			s.lease.renew(ctx)
		}
		return s.srv != nil
	}
	s.took = true
	for _, srv := range s.servers {
		if l, ok := srv.leases.take(ctx); ok {
			s.srv, s.lease = srv, l
			return true
		}
	}
	return false
}

func (s *Stream) frame(ctx context.Context, slot [9]byte, deadline time.Time, skip bool) ([9]byte, string) {
	if s.from == CodecCodec2 {
		pcm, err := s.c2.Decode(s.pcm[:0], slot)
		if err != nil {
			return vocoder.AMBESilenceSlot(), ReasonCodec2
		}
		if skip {
			return vocoder.AMBESilenceSlot(), ReasonTimeout
		}
		var out [9]byte
		reason := s.withAMBE(ctx, deadline, func(c *dvsi.Codec) error {
			var err error
			out, err = c.Encode(pcm)
			return err //nolint:wrapcheck
		})
		if reason != "" {
			return vocoder.AMBESilenceSlot(), reason
		}
		return out, ""
	}
	if skip {
		return s.c2.Silence(), ReasonTimeout
	}
	var pcm []int16
	reason := s.withAMBE(ctx, deadline, func(c *dvsi.Codec) error {
		var err error
		pcm, err = c.Decode(s.pcm[:0], slot)
		return err //nolint:wrapcheck
	})
	if reason != "" {
		return s.c2.Silence(), reason
	}
	out, err := s.c2.Encode(pcm)
	if err != nil {
		return s.c2.Silence(), ReasonCodec2
	}
	return out, ""
}

// withAMBE runs op on the AMBE server. It tries a failed op once more when
// the deadline leaves time for it, except after a burst that the server
// did not answer, and returns why it failed, or "". After a timeout the
// codec first resynchronizes, which takes a second request, so a try then
// needs two AttemptTimeouts. A frame that the deadline leaves no time for
// is ReasonLate and does not count against the server.
func (s *Stream) withAMBE(ctx context.Context, deadline time.Time, op func(*dvsi.Codec) error) string {
	tries := 2
	if s.failing {
		tries = 1
	}
	reason := ReasonLate
	tried := false
	for range tries {
		if ctx.Err() != nil {
			break
		}
		if s.ambe == nil {
			if time.Until(deadline) < s.attempt {
				break
			}
			tried = true
			if err := s.open(ctx, deadline); err != nil {
				reason = reasonOf(err)
				continue
			}
		}
		need := s.attempt
		if s.stale {
			need = 2 * s.attempt
		}
		if time.Until(deadline) < need {
			break
		}
		tried = true
		err := op(s.ambe)
		if err == nil {
			s.failing, s.stale = false, false
			s.srv.answer()
			return ""
		}
		reason = reasonOf(err)
		s.stale = errors.Is(err, dvsi.ErrTimeout)
		if !s.stale {
			s.closeAMBE()
		}
	}
	if tried {
		s.failing = true
	} else {
		reason = ReasonLate
	}
	return reason
}

func (s *Stream) open(ctx context.Context, deadline time.Time) error {
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", s.srv.Addr)
	if err != nil {
		return err //nolint:wrapcheck
	}
	c, err := dvsi.New(conn, &dvsi.Config{Timeout: s.attempt, NoReset: true})
	if err != nil {
		_ = conn.Close()
		return err //nolint:wrapcheck
	}
	s.conn, s.ambe, s.stale = conn, c, false
	return nil
}

func reasonOf(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, dvsi.ErrTimeout), errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return ReasonTimeout
	case errors.Is(err, dvsi.ErrStatus):
		return ReasonError
	case errors.Is(err, syscall.ECONNREFUSED), errors.As(err, &ne):
		return ReasonUnreachable
	default:
		return ReasonError
	}
}
