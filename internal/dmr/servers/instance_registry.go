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

package servers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
)

const (
	instanceKeyPrefix = "dmrhub:instance:"
	// instanceTTL is the TTL for instance registration keys. Must be longer
	// than the heartbeat interval so keys stay alive while running.
	instanceTTL = 30 * time.Second
	// instanceHeartbeat is how often each instance refreshes its TTL.
	instanceHeartbeat = 10 * time.Second
)

// InstanceRegistry tracks running DMRHub server instances in the shared KV
// store. During shutdown it lets a stopping instance determine whether other
// instances are still alive and can accept handoff traffic. When other
// instances are present, disconnect messages (MSTCL, DeregistrationRequest)
// can be skipped so that peers seamlessly migrate to the remaining instance.
type InstanceRegistry struct {
	kv         kv.KV
	instanceID string
	cancel     context.CancelFunc
}

// NewInstanceRegistry creates a registry entry for this instance and starts a
// background heartbeat to keep the key alive.
func NewInstanceRegistry(ctx context.Context, kv kv.KV, instanceID string) *InstanceRegistry {
	r := &InstanceRegistry{
		kv:         kv,
		instanceID: instanceID,
	}

	key := instanceKeyPrefix + instanceID
	if err := kv.Set(ctx, key, []byte(instanceID)); err != nil {
		slog.Error("failed to register instance in KV", "instanceID", instanceID, "error", err)
	}
	if err := kv.Expire(ctx, key, instanceTTL); err != nil {
		slog.Error("failed to set instance TTL", "instanceID", instanceID, "error", err)
	}

	hbCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	go r.heartbeat(hbCtx)

	slog.Info("Registered instance in KV", "instanceID", instanceID)
	return r
}

func (r *InstanceRegistry) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(instanceHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			key := instanceKeyPrefix + r.instanceID
			// Re-set + expire to keep the key alive
			if err := r.kv.Set(ctx, key, []byte(r.instanceID)); err != nil {
				slog.Warn("instance heartbeat: failed to refresh key", "error", err)
			}
			if err := r.kv.Expire(ctx, key, instanceTTL); err != nil {
				slog.Warn("instance heartbeat: failed to refresh TTL", "error", err)
			}
		}
	}
}

// OtherInstancesExist returns true if any other instance (not this one) has a
// live registration in KV. This is used during shutdown to decide whether to
// send disconnect messages.
func (r *InstanceRegistry) OtherInstancesExist(ctx context.Context) bool {
	keys, _, err := r.kv.Scan(ctx, 0, instanceKeyPrefix+"*", 0)
	if err != nil {
		slog.Warn("failed to scan for other instances", "error", err)
		// If we can't tell, be safe and send disconnect messages
		return false
	}
	myKey := instanceKeyPrefix + r.instanceID
	for _, key := range keys {
		if key != myKey {
			return true
		}
	}
	return false
}

// Deregister removes this instance from the registry and stops the heartbeat.
func (r *InstanceRegistry) Deregister(ctx context.Context) {
	if r.cancel != nil {
		r.cancel()
	}
	key := instanceKeyPrefix + r.instanceID
	if err := r.kv.Delete(ctx, key); err != nil {
		slog.Warn("failed to deregister instance from KV", "instanceID", r.instanceID, "error", err)
	}
	slog.Info("Deregistered instance from KV", "instanceID", r.instanceID)
}

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// gracefulHandoffKey is the context key for the graceful-handoff flag.
var gracefulHandoffKey = contextKey{} //nolint:gochecknoglobals

// WithGracefulHandoff returns a copy of ctx with the graceful-handoff flag set.
func WithGracefulHandoff(ctx context.Context, graceful bool) context.Context {
	return context.WithValue(ctx, gracefulHandoffKey, graceful)
}

// IsGracefulHandoff returns true if the context carries a graceful-handoff flag.
func IsGracefulHandoff(ctx context.Context) bool {
	v, _ := ctx.Value(gracefulHandoffKey).(bool)
	return v
}

// GenerateInstanceID creates a unique instance identifier using random bytes.
func GenerateInstanceID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random instance ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}
