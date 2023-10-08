// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

package openbridge

import (
	"context"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"k8s.io/klog/v2"
)

var subscriptionManager *SubscriptionManager //nolint:golint,gochecknoglobals

type SubscriptionManager struct {
	subscriptions           map[uint]context.CancelFunc
	subscriptionsMutex      *sync.RWMutex
	subscriptionCancelMutex map[uint]*sync.RWMutex
}

func GetSubscriptionManager() *SubscriptionManager {
	if subscriptionManager == nil {
		subscriptionManager = &SubscriptionManager{
			subscriptions:           make(map[uint]context.CancelFunc),
			subscriptionsMutex:      &sync.RWMutex{},
			subscriptionCancelMutex: make(map[uint]*sync.RWMutex),
		}
	}
	return subscriptionManager
}

func (m *SubscriptionManager) CancelSubscription(p models.Peer) {
	m.subscriptionsMutex.RLock()
	m.subscriptionCancelMutex[p.ID].RLock()
	cancel, ok := m.subscriptions[p.ID]
	m.subscriptionCancelMutex[p.ID].RUnlock()
	m.subscriptionsMutex.RUnlock()
	if ok {
		m.subscriptionsMutex.Lock()
		m.subscriptionCancelMutex[p.ID].Lock()
		delete(m.subscriptions, p.ID)
		m.subscriptionCancelMutex[p.ID].Unlock()
		delete(m.subscriptionCancelMutex, p.ID)
		m.subscriptionsMutex.Unlock()
		cancel()
	}
}

func (m *SubscriptionManager) Subscribe(ctx context.Context, redis *redis.Client, p models.Peer) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	if !p.Ingress {
		return
	}
	m.subscriptionsMutex.RLock()
	_, ok := m.subscriptions[p.ID]
	m.subscriptionsMutex.RUnlock()
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		m.subscriptionsMutex.Lock()
		_, ok = m.subscriptionCancelMutex[p.ID]
		if !ok {
			m.subscriptionCancelMutex[p.ID] = &sync.RWMutex{}
		}
		m.subscriptionCancelMutex[p.ID].Lock()
		m.subscriptions[p.ID] = cancel
		m.subscriptionCancelMutex[p.ID].Unlock()
		m.subscriptionsMutex.Unlock()
		go m.subscribe(newCtx, redis, p) //nolint:golint,contextcheck
	}
}

func (m *SubscriptionManager) subscribe(ctx context.Context, redis *redis.Client, p models.Peer) {
	if config.GetConfig().Debug {
		klog.Infof("Listening for calls on peer %d", p.ID)
	}
	pubsub := redis.Subscribe(ctx, "openbridge:packets")
	defer func() {
		err := pubsub.Unsubscribe(ctx, "openbridge:packets")
		if err != nil {
			klog.Errorf("Error unsubscribing from openbridge:packets: %s", err)
		}
		err = pubsub.Close()
		if err != nil {
			klog.Errorf("Error closing pubsub connection: %s", err)
		}
	}()
	pubsubChannel := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			if config.GetConfig().Debug {
				klog.Info("Context canceled, stopping subscription to openbridge:packets")
			}
			m.subscriptionsMutex.Lock()
			_, ok := m.subscriptionCancelMutex[p.ID]
			if ok {
				m.subscriptionCancelMutex[p.ID].Lock()
			}
			delete(m.subscriptions, p.ID)
			if ok {
				m.subscriptionCancelMutex[p.ID].Unlock()
				delete(m.subscriptionCancelMutex, p.ID)
			}
			m.subscriptionsMutex.Unlock()
			return
		case msg := <-pubsubChannel:
			rawPacket := models.RawDMRPacket{}
			_, err := rawPacket.UnmarshalMsg([]byte(msg.Payload))
			if err != nil {
				klog.Errorf("Failed to unmarshal raw packet: %s", err)
				continue
			}
			packet, ok := models.UnpackPacket(rawPacket.Data)
			if !ok {
				klog.Errorf("Failed to unpack packet: %s", err)
				continue
			}

			if packet.Repeater == p.ID {
				continue
			}

			packet.Repeater = p.ID
			packet.Slot = false
			redis.Publish(ctx, "openbridge:outgoing", packet.Encode())
		}
	}
}
