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

package netscheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/go-co-op/gocron/v2"
	"gorm.io/gorm"
)

const lockTTL = 30 * time.Second

// maxDurationMinutes is the largest number of minutes that can be safely
// converted to time.Duration without overflowing int64.
const maxDurationMinutes = uint(math.MaxInt64 / int64(time.Minute))

// NetScheduler manages gocron jobs for ScheduledNet entries and handles
// auto-close timers for nets with a configured duration.
type NetScheduler struct {
	scheduler       gocron.Scheduler
	db              *gorm.DB
	pubsub          pubsub.PubSub
	kv              kv.KV
	mu              sync.Mutex
	jobs            map[uint]gocron.Job
	autoCloseTimers sync.Map // map[uint]*time.Timer — keyed by Net ID
}

// NewNetScheduler creates a new NetScheduler.
func NewNetScheduler(db *gorm.DB, ps pubsub.PubSub, kvStore kv.KV) *NetScheduler {
	s, err := gocron.NewScheduler()
	if err != nil {
		slog.Error("Failed to create gocron scheduler for nets", "error", err)
		return nil
	}
	return &NetScheduler{
		scheduler: s,
		db:        db,
		pubsub:    ps,
		kv:        kvStore,
		jobs:      make(map[uint]gocron.Job),
	}
}

// Start starts the underlying gocron scheduler.
func (ns *NetScheduler) Start() {
	ns.scheduler.Start()
}

// Stop stops the underlying gocron scheduler and cancels all auto-close timers.
func (ns *NetScheduler) Stop() {
	if err := ns.scheduler.StopJobs(); err != nil {
		slog.Error("Failed to stop net scheduler jobs", "error", err)
	}
	if err := ns.scheduler.Shutdown(); err != nil {
		slog.Error("Failed to shut down net scheduler", "error", err)
	}
	// Cancel all auto-close timers.
	ns.autoCloseTimers.Range(func(key, value interface{}) bool {
		if timer, ok := value.(*time.Timer); ok {
			timer.Stop()
		}
		return true
	})
}

// LoadScheduledNets loads all enabled scheduled nets from the database and
// registers a gocron job for each one.
func (ns *NetScheduler) LoadScheduledNets(ctx context.Context) error {
	nets, err := models.FindAllEnabledScheduledNets(ns.db)
	if err != nil {
		return fmt.Errorf("failed to load scheduled nets: %w", err)
	}
	for i := range nets {
		if err := ns.RegisterScheduledNet(ctx, &nets[i]); err != nil {
			slog.Error("Failed to register scheduled net", "id", nets[i].ID, "error", err)
		}
	}
	slog.Info("Loaded scheduled nets", "count", len(nets))
	return nil
}

// RegisterScheduledNet adds or replaces a gocron job for the given ScheduledNet.
func (ns *NetScheduler) RegisterScheduledNet(_ context.Context, sn *models.ScheduledNet) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	// Remove existing job if present.
	if existing, ok := ns.jobs[sn.ID]; ok {
		if err := ns.scheduler.RemoveJob(existing.ID()); err != nil {
			slog.Error("Failed to remove existing scheduled net job", "id", sn.ID, "error", err)
		}
	}

	cronDef := gocron.CronJob(sn.CronExpression, false)

	snCopy := *sn
	// Use context.Background() because gocron tasks run as background jobs
	// independent of any request or startup context.
	job, err := ns.scheduler.NewJob(
		cronDef,
		gocron.NewTask(ns.startNetFromSchedule, context.Background(), &snCopy),
		gocron.WithName(fmt.Sprintf("scheduled-net-%d", sn.ID)),
	)
	if err != nil {
		return fmt.Errorf("failed to create gocron job for scheduled net %d: %w", sn.ID, err)
	}

	ns.jobs[sn.ID] = job

	// Update NextRun in the database.
	nextRun, nextErr := job.NextRun()
	if nextErr == nil {
		sn.NextRun = &nextRun
		if updateErr := models.UpdateScheduledNet(ns.db, sn); updateErr != nil {
			slog.Error("Failed to update next_run for scheduled net", "id", sn.ID, "error", updateErr)
		}
	}

	return nil
}

// UnregisterScheduledNet removes the gocron job for the given scheduled net ID.
func (ns *NetScheduler) UnregisterScheduledNet(id uint) {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	if job, ok := ns.jobs[id]; ok {
		if err := ns.scheduler.RemoveJob(job.ID()); err != nil {
			slog.Error("Failed to remove scheduled net job", "id", id, "error", err)
		}
		delete(ns.jobs, id)
	}
}

// ScheduleAutoClose schedules an auto-close timer for a net with a duration.
func (ns *NetScheduler) ScheduleAutoClose(netID uint, duration time.Duration) {
	timer := time.AfterFunc(duration, func() {
		ns.autoCloseNet(netID)
	})
	ns.autoCloseTimers.Store(netID, timer)
}

// CancelAutoClose cancels an auto-close timer for a net.
func (ns *NetScheduler) CancelAutoClose(netID uint) {
	if val, loaded := ns.autoCloseTimers.LoadAndDelete(netID); loaded {
		if timer, ok := val.(*time.Timer); ok {
			timer.Stop()
		}
	}
}

// startNetFromSchedule is the callback for a gocron job. It acquires a
// distributed lock so that only one instance starts the net.
func (ns *NetScheduler) startNetFromSchedule(ctx context.Context, sn *models.ScheduledNet) {
	lockKey := fmt.Sprintf("dmrhub:netscheduler:lock:%d", sn.ID)

	acquired, err := ns.kv.SetNX(ctx, lockKey, "1", lockTTL)
	if err != nil {
		slog.Error("Failed to acquire lock for scheduled net", "id", sn.ID, "error", err)
		return
	}
	if !acquired {
		slog.Debug("Another instance is starting this scheduled net", "id", sn.ID)
		return
	}

	// Check no active net already exists for this talkgroup.
	if _, findErr := models.FindActiveNetForTalkgroup(ns.db, sn.TalkgroupID); findErr == nil {
		slog.Info("Talkgroup already has an active net, skipping scheduled start", "talkgroup_id", sn.TalkgroupID)
		return
	}

	net := models.Net{
		TalkgroupID:     sn.TalkgroupID,
		StartedByUserID: sn.CreatedByUserID,
		ScheduledNetID:  &sn.ID,
		StartTime:       time.Now(),
		DurationMinutes: sn.DurationMinutes,
		Description:     sn.Description,
		Active:          true,
		Showcase:        sn.Showcase,
	}
	if err := models.CreateNet(ns.db, &net); err != nil {
		slog.Error("Failed to create net from schedule", "scheduled_net_id", sn.ID, "error", err)
		return
	}

	// Reload with associations for the event.
	net, err = models.FindNetByID(ns.db, net.ID)
	if err != nil {
		slog.Error("Failed to reload net after creation", "net_id", net.ID, "error", err)
		return
	}

	ns.publishNetEvent(&net, "started")

	// If the scheduled net has a duration, schedule auto-close.
	if sn.DurationMinutes != nil && *sn.DurationMinutes > 0 {
		dur := min(*sn.DurationMinutes, maxDurationMinutes)
		ns.ScheduleAutoClose(net.ID, time.Duration(dur)*time.Minute) //nolint:gosec // bounded by min
	}

	// Update NextRun in the database.
	ns.mu.Lock()
	if job, ok := ns.jobs[sn.ID]; ok {
		if nextRun, nextErr := job.NextRun(); nextErr == nil {
			sn.NextRun = &nextRun
			if updateErr := models.UpdateScheduledNet(ns.db, sn); updateErr != nil {
				slog.Error("Failed to update next_run after net start", "id", sn.ID, "error", updateErr)
			}
		}
	}
	ns.mu.Unlock()
}

// autoCloseNet ends a net when its duration expires.
func (ns *NetScheduler) autoCloseNet(netID uint) {
	ns.autoCloseTimers.Delete(netID)

	if err := models.EndNet(ns.db, netID); err != nil {
		slog.Error("Failed to auto-close net", "net_id", netID, "error", err)
		return
	}

	net, err := models.FindNetByID(ns.db, netID)
	if err != nil {
		slog.Error("Failed to reload net after auto-close", "net_id", netID, "error", err)
		return
	}

	ns.publishNetEvent(&net, "stopped")
}

// publishNetEvent publishes a net start/stop event to pubsub.
func (ns *NetScheduler) publishNetEvent(net *models.Net, event string) {
	evt := apimodels.WSNetEventResponse{
		NetID:       net.ID,
		TalkgroupID: net.TalkgroupID,
		Talkgroup: apimodels.WSCallResponseTalkgroup{
			ID:          net.Talkgroup.ID,
			Name:        net.Talkgroup.Name,
			Description: net.Talkgroup.Description,
		},
		Event:     event,
		Active:    net.Active,
		StartTime: net.StartTime,
		EndTime:   net.EndTime,
	}

	data, err := json.Marshal(evt)
	if err != nil {
		slog.Error("Failed to marshal net event", "error", err)
		return
	}

	topic := fmt.Sprintf("net:events:%d", net.TalkgroupID)
	if err := ns.pubsub.Publish(topic, data); err != nil {
		slog.Error("Failed to publish net event", "error", err)
	}
	// Also publish to the general topic for the WebSocket.
	if err := ns.pubsub.Publish("net:events", data); err != nil {
		slog.Error("Failed to publish net event to general topic", "error", err)
	}
}
