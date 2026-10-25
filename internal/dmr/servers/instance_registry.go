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
	"sync/atomic"
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
	// drainingValue is the value of the key of an instance that drains.
	drainingValue = "draining"
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
	draining   atomic.Bool
}

// NewInstanceRegistry creates a registry entry for this instance and starts a
// background heartbeat to keep the key alive.
func NewInstanceRegistry(ctx context.Context, kv kv.KV, instanceID string) *InstanceRegistry {
	r := &InstanceRegistry{
		kv:         kv,
		instanceID: instanceID,
	}

	if err := r.write(ctx); err != nil {
		slog.Error("failed to register instance in KV", "instanceID", instanceID, "error", err)
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
			if err := r.write(ctx); err != nil {
				slog.Warn("instance heartbeat: failed to refresh key", "error", err)
			}
		}
	}
}

// write stores the key of the instance, with its state, for instanceTTL.
func (r *InstanceRegistry) write(ctx context.Context) error {
	key := instanceKeyPrefix + r.instanceID
	value := r.instanceID
	if r.draining.Load() {
		value = drainingValue
	}
	if err := r.kv.Set(ctx, key, []byte(value)); err != nil {
		return fmt.Errorf("failed to set the key: %w", err)
	}
	if err := r.kv.Expire(ctx, key, instanceTTL); err != nil {
		return fmt.Errorf("failed to set the TTL: %w", err)
	}
	return nil
}

// ID returns the ID of the instance.
func (r *InstanceRegistry) ID() string {
	return r.instanceID
}

// MarkDraining records that the instance drains: it still serves its
// sessions, but stops soon, so another instance may take them over.
func (r *InstanceRegistry) MarkDraining(ctx context.Context) {
	r.draining.Store(true)
	if err := r.write(ctx); err != nil {
		slog.Error("failed to mark the instance as draining", "instanceID", r.instanceID, "error", err)
	}
}

// InstanceDraining reports whether the instance id is registered and
// drains. It is false for an instance that serves, one that stopped or
// died, and when the KV store cannot be read, so that only a replica that
// is known to drain hands its sessions over.
func InstanceDraining(ctx context.Context, store kv.KV, id string) bool {
	v, err := store.Get(ctx, instanceKeyPrefix+id)
	return err == nil && string(v) == drainingValue
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
