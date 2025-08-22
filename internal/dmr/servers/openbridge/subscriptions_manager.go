// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"log/slog"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"go.opentelemetry.io/otel"
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

func (m *SubscriptionManager) Subscribe(ctx context.Context, pubsub pubsub.PubSub, p models.Peer) {
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
		go m.subscribe(newCtx, pubsub, p) //nolint:golint,contextcheck
	}
}

func (m *SubscriptionManager) subscribe(ctx context.Context, pubsub pubsub.PubSub, p models.Peer) {
	slog.Debug("Listening for calls on peer", "peerID", p.ID)
	subscription := pubsub.Subscribe("openbridge:packets")
	defer func() {
		err := subscription.Unsubscribe()
		if err != nil {
			logging.Errorf("Error unsubscribing from openbridge:packets: %s", err)
		}
		err = subscription.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub connection: %s", err)
		}
	}()
	pubsubChannel := subscription.Channel()

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Context canceled, stopping subscription to openbridge:packets", "peerID", p.ID)
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
			_, err := rawPacket.UnmarshalMsg(msg)
			if err != nil {
				logging.Errorf("Failed to unmarshal raw packet: %s", err)
				continue
			}
			packet, ok := models.UnpackPacket(rawPacket.Data)
			if !ok {
				logging.Errorf("Failed to unpack packet: %s", err)
				continue
			}

			if packet.Repeater == p.ID {
				continue
			}

			packet.Repeater = p.ID
			packet.Slot = false
			if err := pubsub.Publish("openbridge:outgoing", packet.Encode()); err != nil {
				logging.Errorf("Error publishing packet to openbridge:outgoing: %v", err)
			}
		}
	}
}
