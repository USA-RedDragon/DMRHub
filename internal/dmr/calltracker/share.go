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

package calltracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"gorm.io/gorm/clause"
)

const (
	voiceRecordPrefix = "calltracker:voice:"
	saveTries         = 3
	saveHold          = 5 * time.Second
	touchEvery        = voiceHang / 2
	partLive          = voiceHang + touchEvery + kvTimeout
)

var errUnchanged = errors.New("the record stays as it is")

type voiceRecord struct {
	ID       uint                  `json:"id,omitempty"`
	Start    time.Time             `json:"start"`
	Chan     string                `json:"chan"`
	Saved    bool                  `json:"saved,omitempty"`
	Gen      uint64                `json:"gen,omitempty"`
	Saving   uint64                `json:"saving,omitempty"`
	SavingAt time.Time             `json:"saving_at,omitzero"`
	Parts    map[string]*voicePart `json:"parts"`
}

type voicePart struct {
	Active bool         `json:"active,omitempty"`
	Seq    uint         `json:"seq"`
	Last   time.Time    `json:"last"`
	Term   bool         `json:"term,omitempty"`
	Held   []voiceDelta `json:"held,omitempty"`
}

type voiceDelta struct {
	ID            string    `json:"id"`
	First         time.Time `json:"first"`
	Last          time.Time `json:"last"`
	Packets       uint      `json:"packets,omitempty"`
	Lost          uint      `json:"lost,omitempty"`
	Bits          uint      `json:"bits,omitempty"`
	Errors        int       `json:"errors,omitempty"`
	Jitter        float32   `json:"jitter,omitempty"`
	RSSI          float32   `json:"rssi,omitempty"`
	Data          []byte    `json:"data,omitempty"`
	FID           *uint8    `json:"fid,omitempty"`
	SubKind       string    `json:"sub_kind,omitempty"`
	Codec         string    `json:"codec,omitempty"`
	Mode          int       `json:"mode,omitempty"`
	MissedMS      int       `json:"missed_ms,omitempty"`
	SentAMBE      int       `json:"sent_ambe,omitempty"`
	SentCodec2    int       `json:"sent_codec2,omitempty"`
	Busy          bool      `json:"busy,omitempty"`
	Undeliverable []string  `json:"undeliverable,omitempty"`
}

type given struct {
	packets, lost, bits  uint
	errors               int
	data                 int
	missed, ambe, codec2 int
	from                 time.Time
}

type claimVerdict int

const (
	claimNew claimVerdict = iota
	claimAdopt
	claimDrop
)

type finishVerdict int

const (
	finishDefer finishVerdict = iota
	finishSave
	finishDelete
	finishDirect
)

func voiceRecordKey(k voiceKey) string {
	return fmt.Sprintf("%s%016x", voiceRecordPrefix, uint64(k))
}

func chanName(k chanKey) string {
	return fmt.Sprintf("%s/%d/%t/%d", k.server, k.repeater, k.slot, k.stream)
}

func loadRecord(old []byte) *voiceRecord {
	if old == nil {
		return nil
	}
	var rec voiceRecord
	if json.Unmarshal(old, &rec) != nil {
		return nil
	}
	if rec.Parts == nil {
		rec.Parts = map[string]*voicePart{}
	}
	maps.DeleteFunc(rec.Parts, func(_ string, p *voicePart) bool { return p == nil })
	return &rec
}

func (rec *voiceRecord) anyActive(except string, now time.Time) bool {
	for id, p := range rec.Parts {
		if id != except && p.Active && now.Sub(p.Last) < partLive {
			return true
		}
	}
	return false
}

func (rec *voiceRecord) latest() *voicePart {
	var last *voicePart
	for _, p := range rec.Parts {
		if last == nil || p.Last.After(last.Last) {
			last = p
		}
	}
	return last
}

func (rec *voiceRecord) allHeld() []voiceDelta {
	var held []voiceDelta
	for _, p := range rec.Parts {
		held = append(held, p.Held...)
	}
	return held
}

func (rec *voiceRecord) dropHeld(saved []voiceDelta) {
	ids := make(map[string]bool, len(saved))
	for _, d := range saved {
		ids[d.ID] = true
	}
	for _, p := range rec.Parts {
		p.Held = slices.DeleteFunc(p.Held, func(d voiceDelta) bool { return ids[d.ID] })
	}
}

func (rec *voiceRecord) saving(now time.Time) bool {
	return rec.Saving != 0 && now.Sub(rec.SavingAt) < saveHold
}

func fresh(ch string, me string, seq uint, at time.Time) *voiceRecord {
	return &voiceRecord{Chan: ch, Start: at, Parts: map[string]*voicePart{me: {Active: true, Seq: seq, Last: at}}}
}

func claimRecord(rec *voiceRecord, me, ch string, seq uint, header bool, at time.Time) (*voiceRecord, claimVerdict) {
	if rec == nil || rec.Chan != ch || header {
		return fresh(ch, me, seq, at), claimNew
	}
	join := func(v claimVerdict) (*voiceRecord, claimVerdict) {
		rec.Parts[me] = &voicePart{Active: true, Seq: seq, Last: at, Held: partHeld(rec, me)}
		return rec, v
	}
	if rec.ID == 0 {
		return join(claimNew)
	}
	if rec.anyActive("", at) {
		return join(claimAdopt)
	}
	last := rec.latest()
	if last == nil {
		return fresh(ch, me, seq, at), claimNew
	}
	if heardBefore(last.Seq, seq) {
		if at.Sub(last.Last) < voiceHang {
			return rec, claimDrop
		}
		return fresh(ch, me, seq, at), claimNew
	}
	if last.Term {
		return fresh(ch, me, seq, at), claimNew
	}
	return join(claimAdopt)
}

func partHeld(rec *voiceRecord, me string) []voiceDelta {
	if p := rec.Parts[me]; p != nil {
		return p.Held
	}
	return nil
}

func installRecord(rec *voiceRecord, me, ch string, id uint, start time.Time, seq uint) (*voiceRecord, uint, time.Time) {
	if rec == nil || rec.Chan != ch {
		rec = fresh(ch, me, seq, start)
	}
	if rec.ID == 0 {
		rec.ID, rec.Start = id, start
	}
	if p := rec.Parts[me]; p == nil || !p.Active {
		rec.Parts[me] = &voicePart{Active: true, Seq: seq, Last: start, Held: partHeld(rec, me)}
	}
	return rec, rec.ID, rec.Start
}

type finishResult struct {
	verdict finishVerdict
	gen     uint64
	held    []voiceDelta
}

func finishRecord(rec *voiceRecord, me, ch string, id uint, seq uint, last time.Time, term, keyUp bool, mine voiceDelta, now time.Time) (*voiceRecord, finishResult) {
	if rec != nil && rec.ID != id {
		return nil, finishResult{verdict: finishDirect}
	}
	if rec == nil {
		rec = &voiceRecord{ID: id, Chan: ch, Start: mine.First, Saved: true, Parts: map[string]*voicePart{}}
	}
	p := rec.Parts[me]
	if p == nil {
		p = &voicePart{}
		rec.Parts[me] = p
	}
	p.Active, p.Seq, p.Last, p.Term = false, seq, last, term
	if keyUp && len(rec.Parts) == 1 && !rec.Saved && len(p.Held) == 0 {
		return rec, finishResult{verdict: finishDelete}
	}
	p.Held = append(p.Held, mine)
	if rec.anyActive(me, now) || rec.saving(now) {
		return rec, finishResult{verdict: finishDefer}
	}
	rec.Gen++
	rec.Saving, rec.SavingAt = rec.Gen, now
	return rec, finishResult{verdict: finishSave, gen: rec.Gen, held: rec.allHeld()}
}

type savedResult struct {
	again  bool
	gen    uint64
	held   []voiceDelta
	active bool
}

func savedRecord(rec *voiceRecord, gen uint64, ok bool, saved []voiceDelta, tries int, now time.Time) (*voiceRecord, savedResult) {
	if rec == nil || rec.Saving != gen {
		return nil, savedResult{}
	}
	if ok {
		rec.Saved = true
		rec.dropHeld(saved)
	}
	res := savedResult{active: rec.anyActive("", now)}
	rec.Saving, rec.SavingAt = 0, time.Time{}
	if !ok || tries >= saveTries || res.active {
		return rec, res
	}
	held := rec.allHeld()
	if len(held) == 0 {
		return rec, res
	}
	rec.Gen++
	rec.Saving, rec.SavingAt = rec.Gen, now
	res.again, res.gen, res.held = true, rec.Gen, held
	return rec, res
}

func (c *CallTracker) updateRecord(key voiceKey, fn func(*voiceRecord) *voiceRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
	defer cancel()
	err := c.kv.Update(ctx, voiceRecordKey(key), streamIdle, func(old []byte) ([]byte, error) {
		rec := fn(loadRecord(old))
		if rec == nil {
			return nil, errUnchanged
		}
		return json.Marshal(rec)
	})
	if errors.Is(err, errUnchanged) {
		return nil
	}
	if err != nil {
		c.metrics.IncrementTrackerShareFailed()
		return fmt.Errorf("sharing a voice call: %w", err)
	}
	return nil
}

func (s *shard) claimVoice(t *task, ch *channel, key voiceKey, e *row) (*row, bool) {
	p := &t.p
	me, name := s.t.replica, chanName(ch.key)
	var verdict claimVerdict
	var id uint
	var start time.Time
	if err := s.t.updateRecord(key, func(rec *voiceRecord) *voiceRecord {
		rec, verdict = claimRecord(rec, me, name, p.Seq, isVoiceHeader(p), t.at)
		id, start = rec.ID, rec.Start
		return rec
	}); err != nil {
		slog.Warn("CallTracker: the voice call stays on this replica", "error", err)
		return nil, false
	}
	switch verdict {
	case claimNew:
	case claimDrop:
		return nil, true
	case claimAdopt:
		if e != nil && e.call.ID == id && !e.deleted {
			s.resume(key, e)
			e.shared, e.deferred = true, false
			e.given.from = t.at
			return e, true
		}
		r := s.adopt(t, ch, key, id, start)
		return r, true
	}
	r := s.open(t, ch, key, models.CallKindVoice, "", voiceHang)
	if r == nil {
		return nil, true
	}
	r.call.LastFrameNum = dmrconst.VoiceA
	s.t.remember(s, r)
	var got uint
	var gotStart time.Time
	if err := s.t.updateRecord(key, func(rec *voiceRecord) *voiceRecord {
		rec, got, gotStart = installRecord(rec, me, name, r.call.ID, r.call.StartTime, p.Seq)
		return rec
	}); err != nil {
		slog.Warn("CallTracker: the voice call stays on this replica", "error", err)
		return r, true
	}
	r.shared = true
	r.given.from = t.at
	if got != r.call.ID {
		if err := s.t.db.Unscoped().Delete(&models.Call{}, r.call.ID).Error; err != nil {
			slog.Error("CallTracker: error deleting a row that another replica made first", "error", err)
		}
		r.call.ID, r.call.StartTime = got, gotStart
		s.markActive(got)
	}
	return r, true
}

func (s *shard) adopt(t *task, ch *channel, key voiceKey, id uint, start time.Time) *row {
	call := s.newCall(t, models.CallKindVoice, "")
	call.ID, call.StartTime = id, start
	s.t.resolve(call, t.peer)
	r := &row{call: call, ch: ch, key: key, last: t.at, hang: voiceHang, shared: true}
	r.given.from = t.at
	r.call.LastFrameNum = dmrconst.VoiceA
	s.rows[key] = r
	s.note(t, r)
	s.markActive(id)
	slog.Debug("CallTracker: transmission moved here", "streamID", call.StreamID, "src", call.SourceID, "dst", call.DestinationID, "id", id)
	return r
}

func (s *shard) markActive(id uint) {
	if err := s.t.db.Model(&models.Call{}).Where("id = ?", id).Update("active", true).Error; err != nil {
		slog.Error("CallTracker: error marking a call active", "error", err)
	}
}

func (r *row) delta() voiceDelta {
	c := r.call
	d := voiceDelta{
		First:         r.given.from,
		Last:          r.last,
		Packets:       c.TotalPackets - r.given.packets,
		Lost:          c.LostSequences - r.given.lost,
		Bits:          c.TotalBits - r.given.bits,
		Errors:        c.TotalErrors - r.given.errors,
		Jitter:        c.Jitter,
		RSSI:          c.RSSI,
		FID:           c.FID,
		SubKind:       c.SubKind,
		Codec:         c.Codec,
		Mode:          c.Codec2Mode,
		MissedMS:      c.MissedMS - r.given.missed,
		SentAMBE:      c.SentAMBE - r.given.ambe,
		SentCodec2:    c.SentCodec2 - r.given.codec2,
		Busy:          c.TranscodeBusy,
		Undeliverable: slices.Clone(c.Undeliverable),
	}
	if r.given.data < len(c.CallData) {
		d.Data = slices.Clone([]byte(c.CallData[r.given.data:]))
	}
	return d
}

func (r *row) markGiven() {
	c := r.call
	r.given = given{packets: c.TotalPackets, lost: c.LostSequences, bits: c.TotalBits, errors: c.TotalErrors, data: len(c.CallData), missed: c.MissedMS, ambe: c.SentAMBE, codec2: c.SentCodec2, from: r.last}
}

func mergeVoice(tmpl *models.Call, base *models.Call, start time.Time, parts []voiceDelta) models.Call {
	out := *tmpl
	out.StartTime = start
	out.Active = false
	out.CallData = nil
	out.TotalPackets, out.LostSequences, out.TotalBits, out.TotalErrors = 0, 0, 0, 0
	out.MissedMS, out.SentAMBE, out.SentCodec2 = 0, 0, 0
	out.Undeliverable = nil
	var jitter, rssi float64
	last := start
	if base != nil {
		out.CallData = slices.Clone(base.CallData)
		out.TotalPackets, out.LostSequences, out.TotalBits, out.TotalErrors = base.TotalPackets, base.LostSequences, base.TotalBits, base.TotalErrors
		out.MissedMS, out.SentAMBE, out.SentCodec2 = base.MissedMS, base.SentAMBE, base.SentCodec2
		out.Undeliverable = slices.Clone(base.Undeliverable)
		out.Response = base.Response
		if base.FID != nil {
			out.FID = base.FID
		}
		if base.SubKind != "" {
			out.SubKind = base.SubKind
		}
		if base.Codec != "" {
			out.Codec, out.Codec2Mode = base.Codec, base.Codec2Mode
		}
		out.TranscodeBusy = out.TranscodeBusy || base.TranscodeBusy
		jitter, rssi = float64(base.Jitter)*float64(base.TotalPackets), float64(base.RSSI)*float64(base.TotalPackets)
		if end := base.StartTime.Add(base.Duration); end.After(last) {
			last = end
		}
	}
	slices.SortStableFunc(parts, func(a, b voiceDelta) int { return a.First.Compare(b.First) })
	for _, d := range parts {
		if len(out.CallData)+len(d.Data) <= maxCallData {
			out.CallData = append(out.CallData, d.Data...)
		}
		out.TotalPackets += d.Packets
		out.LostSequences += d.Lost
		out.TotalBits += d.Bits
		out.TotalErrors += d.Errors
		out.MissedMS += d.MissedMS
		out.SentAMBE += d.SentAMBE
		out.SentCodec2 += d.SentCodec2
		out.TranscodeBusy = out.TranscodeBusy || d.Busy
		jitter += float64(d.Jitter) * float64(d.Packets)
		rssi += float64(d.RSSI) * float64(d.Packets)
		if out.FID == nil {
			out.FID = d.FID
		}
		if d.SubKind != "" {
			out.SubKind = d.SubKind
		}
		if out.Codec == "" && d.Codec != "" {
			out.Codec, out.Codec2Mode = d.Codec, d.Mode
		}
		for _, u := range d.Undeliverable {
			if !slices.Contains(out.Undeliverable, u) {
				out.Undeliverable = append(out.Undeliverable, u)
			}
		}
		if d.Last.After(last) {
			last = d.Last
		}
	}
	out.LastPacketTime = last
	out.Duration = last.Sub(start)
	if out.TotalPackets > 0 {
		out.Loss = float32(out.LostSequences) / float32(out.TotalPackets)
		out.Jitter = float32(jitter / float64(out.TotalPackets))
		out.RSSI = float32(rssi / float64(out.TotalPackets))
	}
	if out.TotalBits > 0 {
		out.BER = float32(out.TotalErrors) / float32(out.TotalBits)
	}
	return out
}

func (s *shard) finishShared(r *row, at time.Time) {
	call := r.call
	term := !at.IsZero()
	keyUp := term && call.Duration < keyUp
	last := r.last
	if term {
		last = at
	}
	mine := r.delta()
	mine.Last = last
	mine.ID = fmt.Sprintf("%s/%d", s.t.replica, s.t.deltas.Add(1))
	key, _ := r.key.(voiceKey)
	me, name := s.t.replica, chanName(r.ch.key)
	now := s.t.clock.Now()
	var res finishResult
	if err := s.t.updateRecord(key, func(rec *voiceRecord) *voiceRecord {
		rec, res = finishRecord(rec, me, name, call.ID, call.LastSeq, last, term, keyUp, mine, now)
		return rec
	}); err != nil {
		slog.Warn("CallTracker: the voice call is added to its row without the other replicas", "error", err)
		s.saveParts(r, []voiceDelta{mine})
		return
	}
	switch res.verdict {
	case finishDelete:
		r.deleted = true
		s.t.db.Unscoped().Delete(call)
		return
	case finishDefer:
		r.markGiven()
		r.deferred = true
		s.waiting[r] = now.Add(partLive)
		slog.Debug("CallTracker: the part of a moved call ended here", "streamID", call.StreamID, "id", call.ID)
		return
	case finishDirect:
		if s.saveParts(r, []voiceDelta{mine}) {
			r.markGiven()
		}
		return
	case finishSave:
	}
	r.markGiven()
	s.saveLoop(r, key, res)
}

func (s *shard) noteDeferred(r *row) {
	key, _ := r.key.(voiceKey)
	d := r.delta()
	d.ID = fmt.Sprintf("%s/%d", s.t.replica, s.t.deltas.Add(1))
	me, id := s.t.replica, r.call.ID
	placed := false
	err := s.t.updateRecord(key, func(rec *voiceRecord) *voiceRecord {
		placed = false
		if rec == nil || rec.ID != id {
			return nil
		}
		p := rec.Parts[me]
		if p == nil {
			p = &voicePart{Last: d.Last}
			rec.Parts[me] = p
		}
		p.Held = append(p.Held, d)
		placed = true
		return rec
	})
	if err != nil || !placed {
		s.saveParts(r, []voiceDelta{d})
		return
	}
	r.markGiven()
	s.waiting[r] = s.t.clock.Now()
}

func retryRecord(rec *voiceRecord, me string, id uint, now time.Time) (*voiceRecord, finishResult, bool) {
	if rec == nil || rec.ID != id {
		return nil, finishResult{}, true
	}
	held := false
	for _, p := range rec.Parts {
		held = held || len(p.Held) > 0
	}
	if !held {
		return nil, finishResult{}, true
	}
	if rec.anyActive(me, now) || rec.saving(now) {
		return nil, finishResult{verdict: finishDefer}, false
	}
	rec.Gen++
	rec.Saving, rec.SavingAt = rec.Gen, now
	return rec, finishResult{verdict: finishSave, gen: rec.Gen, held: rec.allHeld()}, true
}

func (s *shard) retryWaiting(now time.Time) {
	for r, next := range s.waiting {
		if now.Before(next) {
			continue
		}
		key, _ := r.key.(voiceKey)
		if !r.deferred || r.deleted || now.Sub(r.last) >= streamIdle {
			delete(s.waiting, r)
			continue
		}
		var res finishResult
		var done bool
		me, id := s.t.replica, r.call.ID
		if err := s.t.updateRecord(key, func(rec *voiceRecord) *voiceRecord {
			rec, res, done = retryRecord(rec, me, id, now)
			return rec
		}); err != nil {
			s.waiting[r] = now.Add(touchEvery)
			continue
		}
		if !done {
			s.waiting[r] = now.Add(touchEvery)
			continue
		}
		delete(s.waiting, r)
		if res.verdict == finishSave {
			s.saveLoop(r, key, res)
		}
	}
}

func (s *shard) saveLoop(r *row, key voiceKey, res finishResult) {
	call := r.call
	gen, parts := res.gen, res.held
	for try := 1; ; try++ {
		ok := s.saveParts(r, parts)
		var after savedResult
		if err := s.t.updateRecord(key, func(rec *voiceRecord) *voiceRecord {
			rec, after = savedRecord(rec, gen, ok, parts, try, s.t.clock.Now())
			return rec
		}); err != nil {
			slog.Warn("CallTracker: the end of a save of a voice call did not reach the other replicas", "error", err)
		}
		if !ok {
			r.deferred = true
			s.waiting[r] = s.t.clock.Now().Add(touchEvery)
		}
		if after.active {
			s.markActive(call.ID)
		}
		if !after.again {
			return
		}
		gen, parts = after.gen, after.held
	}
}

func (s *shard) saveParts(r *row, parts []voiceDelta) bool {
	out, ok := s.saveMerged(r.call, parts)
	if !ok {
		return false
	}
	s.adoptMerged(r, &out)
	s.t.publishCall(s.idx, &out)
	s.t.publishNetCheckIn(s.idx, &out)
	slog.Info("Transmission ended", "kind", out.Kind, "sub", out.SubKind, "streamID", out.StreamID, "src", out.SourceID, "dst", out.DestinationID, "repeater", out.IngressID, "duration", out.Duration, "loss", out.Loss*pct, "ber", out.BER*pct, "rssi", out.RSSI, "jitter", out.Jitter)
	return true
}

func touchRecord(rec *voiceRecord, me, ch string, id uint, start time.Time, seq uint, last time.Time) *voiceRecord {
	if rec == nil {
		rec = &voiceRecord{ID: id, Chan: ch, Start: start, Saved: true, Parts: map[string]*voicePart{}}
	}
	if rec.ID != id {
		return nil
	}
	p := rec.Parts[me]
	if p == nil {
		p = &voicePart{}
		rec.Parts[me] = p
	}
	p.Active, p.Seq, p.Last = true, seq, last
	return rec
}

func (s *shard) touchShared(now time.Time) {
	for k, r := range s.rows {
		key, ok := k.(voiceKey)
		if !ok || !r.shared || r.finished || now.Sub(r.touched) < touchEvery {
			continue
		}
		r.touched = now
		me, name, call := s.t.replica, chanName(r.ch.key), r.call
		if err := s.t.updateRecord(key, func(rec *voiceRecord) *voiceRecord {
			return touchRecord(rec, me, name, call.ID, call.StartTime, call.LastSeq, r.last)
		}); err != nil {
			slog.Warn("CallTracker: the voice call was not refreshed for the other replicas", "error", err)
		}
	}
}

func (s *shard) saveMerged(call *models.Call, parts []voiceDelta) (models.Call, bool) {
	var base models.Call
	if err := s.t.db.Unscoped().Omit(clause.Associations).First(&base, call.ID).Error; err != nil {
		slog.Error("CallTracker: error reading a call to merge into", "error", err)
		return models.Call{}, false
	}
	out := mergeVoice(call, &base, base.StartTime, parts)
	res := s.t.db.Model(&models.Call{}).Where("id = ? AND duration <= ?", out.ID, out.Duration).
		Select("*").Omit("id", "created_at", "deleted_at", clause.Associations).Updates(&out)
	if res.Error != nil {
		slog.Error("CallTracker: error saving call", "error", res.Error)
		return out, false
	}
	if res.RowsAffected == 0 {
		slog.Warn("CallTracker: a newer save of the call is already stored", "id", out.ID)
		return out, false
	}
	return out, true
}

func (s *shard) adoptMerged(r *row, out *models.Call) {
	*r.call = *out
	r.markGiven()
}
