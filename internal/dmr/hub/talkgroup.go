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

package hub

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

func (h *Hub) queueLink(ctx context.Context, repeaterID uint, fn func()) bool {
	if !h.links.Do(ctx, repeaterID, fn) {
		if ctx.Err() == nil {
			slog.Warn("Dropping a talkgroup link change of a backed-up repeater", "repeaterID", repeaterID)
		}
		return false
	}
	return true
}

func (h *Hub) changeDynamic(ctx context.Context, repeaterID uint, slot bool, at time.Time, change func()) {
	done := make(chan struct{})
	if !h.queueLink(ctx, repeaterID, func() {
		defer close(done)
		h.noteDynamicChange(repeaterID, slot, at)
		change()
	}) {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	case <-h.done:
	}
}

func (h *Hub) noteDynamicChange(repeaterID uint, slot bool, at time.Time) {
	h.dynamicChanges.Compute(repeaterSlot{repeater: repeaterID, slot: slot}, func(cur time.Time, loaded bool) (time.Time, xsync.ComputeOp) {
		if loaded && !at.After(cur) {
			return cur, xsync.CancelOp
		}
		return at, xsync.UpdateOp
	})
}

func (h *Hub) changedSince(repeaterID uint, slot bool, came time.Time) bool {
	at, ok := h.dynamicChanges.Load(repeaterSlot{repeater: repeaterID, slot: slot})
	return ok && !at.Before(came)
}

func (h *Hub) bumpSessionEpoch(repeaterID uint) {
	if h.db == nil {
		return
	}
	var epoch uint64
	err := h.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec("UPDATE repeaters SET session_epoch = session_epoch + 1 WHERE id = ? AND deleted_at IS NULL", repeaterID)
		if res.Error != nil {
			return res.Error
		}
		return tx.Raw("SELECT session_epoch FROM repeaters WHERE id = ? AND deleted_at IS NULL", repeaterID).Scan(&epoch).Error
	})
	if err != nil {
		slog.Error("Cannot raise the session epoch of a repeater", "repeaterID", repeaterID, "error", err)
		h.sessionEpochs.Delete(repeaterID)
		return
	}
	h.sessionEpochs.Store(repeaterID, epoch)
}

func (h *Hub) sessionEpoch(repeaterID uint) uint64 {
	epoch, _ := h.sessionEpochs.Load(repeaterID)
	return epoch
}

type dynamicSeen struct {
	tg    *uint
	epoch uint64
}

func seenOf(r *models.Repeater, slot bool) dynamicSeen {
	if slot {
		return dynamicSeen{tg: r.TS2DynamicTalkgroupID, epoch: r.TS2LinkEpoch}
	}
	return dynamicSeen{tg: r.TS1DynamicTalkgroupID, epoch: r.TS1LinkEpoch}
}

func dynamicColumns(slot bool) (string, string) {
	if slot {
		return "ts2_dynamic_talkgroup_id", "ts2_link_epoch"
	}
	return "ts1_dynamic_talkgroup_id", "ts1_link_epoch"
}

func sameTG(a, b *uint) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (h *Hub) readDynamic(repeaterID uint, slot bool) (dynamicSeen, error) {
	column, epochColumn := dynamicColumns(slot)
	var got struct {
		TG    *uint
		Epoch uint64
	}
	if err := h.db.Raw("SELECT "+column+" AS tg, "+epochColumn+" AS epoch FROM repeaters WHERE id = ? AND deleted_at IS NULL", repeaterID).Scan(&got).Error; err != nil {
		return dynamicSeen{}, fmt.Errorf("reading the dynamic talkgroup of repeater %d: %w", repeaterID, err)
	}
	return dynamicSeen{tg: got.TG, epoch: got.Epoch}, nil
}

func (h *Hub) writeDynamic(repeaterID uint, slot bool, tg *uint, epoch uint64) bool {
	if epoch == 0 {
		seen, err := h.readDynamic(repeaterID, slot)
		if err != nil {
			slog.Error("Error reading the dynamic talkgroup of a repeater", "repeaterID", repeaterID, "error", err)
			return false
		}
		return h.writeDynamicSeen(repeaterID, slot, tg, seen)
	}
	column, epochColumn := dynamicColumns(slot)
	res := h.db.Exec("UPDATE repeaters SET "+column+" = ?, "+epochColumn+" = ? WHERE id = ? AND deleted_at IS NULL AND "+epochColumn+" <= ?", tgValue(tg), epoch, repeaterID, epoch)
	return h.dynamicWritten(res, repeaterID, slot, tg, epoch)
}

func (h *Hub) writeDynamicSeen(repeaterID uint, slot bool, tg *uint, seen dynamicSeen) bool {
	column, epochColumn := dynamicColumns(slot)
	q := "UPDATE repeaters SET " + column + " = ? WHERE id = ? AND deleted_at IS NULL AND " + epochColumn + " = ? AND "
	args := []any{tgValue(tg), repeaterID, seen.epoch}
	if seen.tg == nil {
		q += column + " IS NULL"
	} else {
		q += column + " = ?"
		args = append(args, *seen.tg)
	}
	return h.dynamicWritten(h.db.Exec(q, args...), repeaterID, slot, tg, seen.epoch)
}

func tgValue(tg *uint) any {
	if tg == nil {
		return nil
	}
	return *tg
}

func (h *Hub) dynamicWritten(res *gorm.DB, repeaterID uint, slot bool, tg *uint, epoch uint64) bool {
	if res.Error != nil {
		slog.Error("Error saving the dynamic talkgroup of a repeater", "repeaterID", repeaterID, "error", res.Error)
		return false
	}
	if res.RowsAffected > 0 {
		return true
	}
	got, err := h.readDynamic(repeaterID, slot)
	if err != nil {
		slog.Error("Error reading the dynamic talkgroup of a repeater", "repeaterID", repeaterID, "error", err)
		return false
	}
	return sameTG(got.tg, tg) && got.epoch == epoch
}

// doUnlink handles talkgroup unlinking (TG 4000).
func (h *Hub) doUnlink(ctx context.Context, packet models.Packet, came time.Time, epoch uint64) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.doUnlink")
	defer span.End()

	h.noteDynamicChange(packet.Repeater, packet.Slot, came)
	h.unlinkDynamic(packet.Repeater, packet.Slot, epoch)
}

// UnlinkDynamicTalkgroup clears the dynamic talkgroup of a timeslot of the
// repeater, as a voice call to TG 4000 on that timeslot does. slot true is
// TS2.
func (h *Hub) UnlinkDynamicTalkgroup(ctx context.Context, repeaterID uint, slot bool) {
	h.UnlinkDynamicTalkgroupAt(ctx, repeaterID, slot, h.clock.Now())
}

// UnlinkDynamicTalkgroupAt is UnlinkDynamicTalkgroup for a change that came
// at at, such as when the repeater options that ask for it came. It runs
// after the link changes that the hub queued for the repeater. A burst of
// a call that came here at or before at, or that came to a replica that
// served an earlier session of the repeater, links nothing on the
// timeslot after it.
func (h *Hub) UnlinkDynamicTalkgroupAt(ctx context.Context, repeaterID uint, slot bool, at time.Time) {
	epoch := h.sessionEpoch(repeaterID)
	h.changeDynamic(ctx, repeaterID, slot, at, func() { h.unlinkDynamic(repeaterID, slot, epoch) })
}

func (h *Hub) unlinkDynamic(repeaterID uint, slot bool, epoch uint64) {
	dbRepeater, err := models.FindRepeaterByID(h.db, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater for unlink", "error", err)
		return
	}

	h.unlinkTimeslot(&dbRepeater, slot, epoch)
}

// unlinkDynamicIf clears the dynamic talkgroup of a timeslot of the
// repeater when it is tg.
func (h *Hub) unlinkDynamicIf(repeaterID uint, slot bool, tg uint) {
	h.unlinkDynamicIfAfter(repeaterID, slot, tg, nil)
}

func (h *Hub) unlinkDynamicIfAfter(repeaterID uint, slot bool, tg uint, between func()) {
	dbRepeater, err := models.FindRepeaterByID(h.db, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater for unlink", "error", err)
		return
	}
	current := dbRepeater.TS1DynamicTalkgroupID
	if slot {
		current = dbRepeater.TS2DynamicTalkgroupID
	}
	if current == nil || *current != tg {
		return
	}
	if between != nil {
		between()
	}
	h.clearTimeslot(dbRepeater.ID, slot, current, h.writeDynamicSeen(dbRepeater.ID, slot, nil, seenOf(&dbRepeater, slot)))
}

// unlinkTimeslot clears the dynamic talkgroup for the given timeslot on a repeater.
// slot=true means TS2, slot=false means TS1.
func (h *Hub) unlinkTimeslot(repeater *models.Repeater, slot bool, epoch uint64) {
	current := seenOf(repeater, slot).tg
	h.clearTimeslot(repeater.ID, slot, current, h.writeDynamic(repeater.ID, slot, nil, epoch))
}

func (h *Hub) clearTimeslot(repeaterID uint, slot bool, current *uint, cleared bool) {
	ts := dmrconst.TimeslotOne
	if slot {
		ts = dmrconst.TimeslotTwo
	}
	slog.Info("Unlinking timeslot", "timeslot", ts, "repeaterID", repeaterID)
	h.reloadRepeater(repeaterID)
	if !cleared {
		slog.Info("Not unlinking a timeslot whose link changed since it was read", "timeslot", ts, "repeaterID", repeaterID)
		return
	}
	if current == nil {
		return
	}
	h.unsubscribeTalkgroup(repeaterID, *current, ts)
	h.announceRepeaterChange(repeaterID)
}

// switchDynamicTalkgroup updates the dynamic talkgroup assignment for a repeater
// when it transmits a group call on a talkgroup it isn't currently linked to.
func (h *Hub) switchDynamicTalkgroup(ctx context.Context, packet models.Packet, at, came time.Time, epoch uint64) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.switchDynamicTalkgroup")
	defer span.End()

	h.linkDynamic(ctx, packet.Repeater, packet.Dst, packet.Slot, at, came, epoch)
}

// LinkDynamicTalkgroup links the talkgroup as the dynamic talkgroup of a
// timeslot of the repeater, as a voice call on the talkgroup on that
// timeslot does, and restarts its timeout. slot true is TS2. It links
// nothing while dynamic links are off.
func (h *Hub) LinkDynamicTalkgroup(ctx context.Context, repeaterID uint, talkgroupID uint, slot bool) {
	h.LinkDynamicTalkgroupAt(ctx, repeaterID, talkgroupID, slot, h.clock.Now())
}

// LinkDynamicTalkgroupAt is LinkDynamicTalkgroup for a change that came at
// at, such as when the repeater options that ask for it came. It runs after
// the link changes that the hub queued for the repeater. A burst of a call
// that came here at or before at, or that came to a replica that served an
// earlier session of the repeater, links nothing on the timeslot after it.
func (h *Hub) LinkDynamicTalkgroupAt(ctx context.Context, repeaterID uint, talkgroupID uint, slot bool, at time.Time) {
	epoch := h.sessionEpoch(repeaterID)
	h.changeDynamic(ctx, repeaterID, slot, at, func() {
		h.linkDynamic(ctx, repeaterID, talkgroupID, slot, at, time.Time{}, epoch)
	})
}

// linkDynamic is LinkDynamicTalkgroup for a call that the hub routed at at,
// which restarts the timeout from at.
func (h *Hub) linkDynamic(ctx context.Context, repeaterID uint, talkgroupID uint, slot bool, at, came time.Time, epoch uint64) {
	if h.routingConfig().DisableDynamicLinks {
		slog.Debug("Not linking a dynamic talkgroup, since dynamic links are off", "repeaterID", repeaterID, "talkgroupID", talkgroupID)
		return
	}
	repeater, err := h.waitRepeater(repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", repeaterID, "error", err)
		return
	}

	currentDynTGID, ts := repeater.TS1DynamicTalkgroupID, 1
	if slot {
		currentDynTGID, ts = repeater.TS2DynamicTalkgroupID, 2
	}
	if currentDynTGID != nil && *currentDynTGID == talkgroupID {
		h.noteDynamicUse(repeaterID, slot, talkgroupID, at)
		return
	}
	if !h.mayLink(repeaterID, talkgroupID) {
		slog.Info("Not linking a restricted talkgroup to a repeater that it does not approve", "repeaterID", repeaterID, "talkgroupID", talkgroupID)
		return
	}
	if !came.IsZero() && h.changedSince(repeaterID, slot, came) {
		slog.Info("Not linking the talkgroup of a call that came before a later change of the dynamic talkgroup", "repeaterID", repeaterID, "talkgroupID", talkgroupID, "timeslot", ts)
		return
	}

	talkgroup, err := models.FindTalkgroupByID(h.db, talkgroupID)
	if err != nil {
		slog.Error("Error finding talkgroup", "talkgroupID", talkgroupID, "error", err)
		return
	}

	slog.Info("Dynamically Linking timeslot", "timeslot", ts, "repeaterID", repeaterID, "talkgroupID", talkgroupID)
	if !h.writeDynamic(repeater.ID, slot, &talkgroup.ID, epoch) {
		slog.Info("Not linking the talkgroup of a call from an earlier session of the repeater", "repeaterID", repeaterID, "talkgroupID", talkgroupID, "timeslot", ts)
		h.reloadRepeater(repeater.ID)
		return
	}
	h.noteDynamicUse(repeaterID, slot, talkgroupID, at)
	h.reloadRepeater(repeater.ID)
	h.subscribeTalkgroup(ctx, repeater.ID, talkgroupID)
	h.announceRepeaterChange(repeater.ID)
}

// loadTalkgroups reads the IDs of all talkgroups from the database. When
// the database fails, the hub keeps the IDs that it had.
func (h *Hub) loadTalkgroups() {
	if h.db == nil {
		return
	}
	var ids []uint
	if err := h.db.Model(&models.Talkgroup{}).Pluck("id", &ids).Error; err != nil {
		slog.Error("Cannot load the talkgroups, keeping the ones known", "error", err)
		return
	}
	set := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	h.talkgroupsMu.Lock()
	h.talkgroups.Store(&set)
	h.talkgroupsMu.Unlock()
	var auto []uint
	if err := h.db.Model(&models.Talkgroup{}).Where("auto_created = ?", true).Pluck("id", &auto).Error; err != nil {
		slog.Error("Cannot load the talkgroups that the hub made", "error", err)
		return
	}
	for _, id := range auto {
		h.autoTalkgroups.Store(id, struct{}{})
	}
}

// noteTalkgroup reads the talkgroup id again after a change of it.
func (h *Hub) noteTalkgroup(id uint) {
	if h.db == nil {
		return
	}
	exists, err := models.TalkgroupIDExists(h.db, id)
	if err != nil {
		slog.Error("Cannot read a changed talkgroup, forgetting it", "talkgroupID", id, "error", err)
		h.talkgroupIDs.Drop(id)
		return
	}
	h.talkgroupIDs.Put(id, exists)
	h.noteAutoTalkgroup(id, exists)
	h.talkgroupsMu.Lock()
	defer h.talkgroupsMu.Unlock()
	set := map[uint]struct{}{}
	if old := h.talkgroups.Load(); old != nil {
		set = maps.Clone(*old)
	}
	if exists {
		set[id] = struct{}{}
	} else {
		delete(set, id)
	}
	h.talkgroups.Store(&set)
}

// noteAutoTalkgroup reads again whether the hub made the talkgroup id by
// itself.
func (h *Hub) noteAutoTalkgroup(id uint, exists bool) {
	var auto []uint
	if exists {
		if err := h.db.Model(&models.Talkgroup{}).Where("id = ? AND auto_created = ?", id, true).Pluck("id", &auto).Error; err != nil {
			slog.Error("Cannot read whether the hub made a talkgroup", "talkgroupID", id, "error", err)
			return
		}
	}
	if len(auto) > 0 {
		h.autoTalkgroups.Store(id, struct{}{})
		return
	}
	h.autoTalkgroups.Delete(id)
}

// talkgroupExists reports whether the talkgroup id exists. It waits on the
// database at most lookup.Wait, and only for a talkgroup that the hub did
// not load.
func (h *Hub) talkgroupExists(id uint) (bool, error) {
	if set := h.talkgroups.Load(); set != nil {
		if _, ok := (*set)[id]; ok {
			return true, nil
		}
	}
	exists, err := h.talkgroupIDs.Get(id)
	if err != nil {
		return false, fmt.Errorf("looking up talkgroup %d: %w", id, err)
	}
	return exists, nil
}
