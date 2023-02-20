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

package hbrp

import (
	"context"
	"fmt"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"k8s.io/klog/v2"
)

var subscriptionManager *SubscriptionManager //nolint:golint,gochecknoglobals

type SubscriptionManager struct {
	subscriptions           map[uint]map[uint]context.CancelFunc
	subscriptionsMutex      *sync.RWMutex
	subscriptionCancelMutex map[uint]map[uint]*sync.RWMutex
}

func GetSubscriptionManager() *SubscriptionManager {
	if subscriptionManager == nil {
		subscriptionManager = &SubscriptionManager{
			subscriptions:           make(map[uint]map[uint]context.CancelFunc),
			subscriptionsMutex:      &sync.RWMutex{},
			subscriptionCancelMutex: make(map[uint]map[uint]*sync.RWMutex),
		}
	}
	return subscriptionManager
}

func (m *SubscriptionManager) CancelSubscription(p models.Repeater, talkgroupID uint) {
	m.subscriptionsMutex.RLock()
	m.subscriptionCancelMutex[p.RadioID][talkgroupID].RLock()
	cancel, ok := m.subscriptions[p.RadioID][talkgroupID]
	m.subscriptionCancelMutex[p.RadioID][talkgroupID].RUnlock()
	m.subscriptionsMutex.RUnlock()
	if ok {
		// Check if the talkgroup is already subscribed to on a different slot
		// If it is, don't cancel the subscription
		if p.TS1DynamicTalkgroupID != nil && *p.TS1DynamicTalkgroupID == talkgroupID {
			return
		}
		if p.TS2DynamicTalkgroupID != nil && *p.TS2DynamicTalkgroupID == talkgroupID {
			return
		}
		for _, tg := range p.TS1StaticTalkgroups {
			if tg.ID == talkgroupID {
				return
			}
		}
		for _, tg := range p.TS2StaticTalkgroups {
			if tg.ID == talkgroupID {
				return
			}
		}
		m.subscriptionsMutex.Lock()
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Lock()
		delete(m.subscriptions[p.RadioID], talkgroupID)
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Unlock()
		delete(m.subscriptionCancelMutex[p.RadioID], talkgroupID)
		m.subscriptionsMutex.Unlock()
		cancel()
	}
}

func (m *SubscriptionManager) CancelAllSubscriptions(p models.Repeater) {
	if config.GetConfig().Debug {
		klog.Errorf("Cancelling all newly inactive subscriptions for repeater %d", p.RadioID)
	}
	m.subscriptionsMutex.RLock()
	for tgID := range m.subscriptions[p.RadioID] {
		m.subscriptionsMutex.RUnlock()
		m.CancelSubscription(p, tgID)
		m.subscriptionsMutex.RLock()
	}
	m.subscriptionsMutex.RUnlock()
}

func (m *SubscriptionManager) ListenForCallsOn(ctx context.Context, redis *redis.Client, p models.Repeater, talkgroupID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()
	m.subscriptionsMutex.RLock()
	_, ok := m.subscriptions[p.RadioID][talkgroupID]
	m.subscriptionsMutex.RUnlock()
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		m.subscriptionsMutex.Lock()
		_, ok = m.subscriptionCancelMutex[p.RadioID][talkgroupID]
		if !ok {
			m.subscriptionCancelMutex[p.RadioID][talkgroupID] = &sync.RWMutex{}
		}
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Lock()
		m.subscriptions[p.RadioID][talkgroupID] = cancel
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Unlock()
		m.subscriptionsMutex.Unlock()
		go m.subscribeTG(newCtx, redis, p, talkgroupID) //nolint:golint,contextcheck
	}
}

func (m *SubscriptionManager) ListenForCalls(ctx context.Context, redis *redis.Client, p models.Repeater) {
	// Subscribe to Redis "packets:repeater:<id>" channel for a dmr.RawDMRPacket
	// This channel is used to get private calls headed to this repeater
	// When a packet is received, we need to publish it to "outgoing" channel
	// with the destination repeater ID as this one
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	m.subscriptionsMutex.RLock()
	_, ok := m.subscriptions[p.RadioID]
	m.subscriptionsMutex.RUnlock()
	if !ok {
		m.subscriptionsMutex.Lock()
		m.subscriptions[p.RadioID] = make(map[uint]context.CancelFunc)
		m.subscriptionCancelMutex[p.RadioID] = make(map[uint]*sync.RWMutex)
		m.subscriptionsMutex.Unlock()
	}
	m.subscriptionsMutex.RLock()
	_, ok = m.subscriptions[p.RadioID][p.RadioID]
	m.subscriptionsMutex.RUnlock()
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		m.subscriptionsMutex.Lock()
		_, ok = m.subscriptionCancelMutex[p.RadioID][p.RadioID]
		if !ok {
			m.subscriptionCancelMutex[p.RadioID][p.RadioID] = &sync.RWMutex{}
		}
		m.subscriptionCancelMutex[p.RadioID][p.RadioID].Lock()
		m.subscriptions[p.RadioID][p.RadioID] = cancel
		m.subscriptionCancelMutex[p.RadioID][p.RadioID].Unlock()
		m.subscriptionsMutex.Unlock()
		go m.subscribeRepeater(newCtx, redis, p) //nolint:golint,contextcheck
	}

	// Subscribe to Redis "packets:talkgroup:<id>" channel for each talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		m.subscriptionsMutex.RLock()
		_, ok := m.subscriptions[p.RadioID][tg.ID]
		m.subscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.subscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][tg.ID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][tg.ID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Lock()
			m.subscriptions[p.RadioID][tg.ID] = cancel
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Unlock()
			m.subscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, tg.ID) //nolint:golint,contextcheck
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		m.subscriptionsMutex.RLock()
		_, ok := m.subscriptions[p.RadioID][tg.ID]
		m.subscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.subscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][tg.ID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][tg.ID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Lock()
			m.subscriptions[p.RadioID][tg.ID] = cancel
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Unlock()
			m.subscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, tg.ID) //nolint:golint,contextcheck
		}
	}
	if p.TS1DynamicTalkgroupID != nil {
		m.subscriptionsMutex.RLock()
		_, ok := m.subscriptions[p.RadioID][*p.TS1DynamicTalkgroupID]
		m.subscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.subscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID].Lock()
			m.subscriptions[p.RadioID][*p.TS1DynamicTalkgroupID] = cancel
			m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID].Unlock()
			m.subscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, *p.TS1DynamicTalkgroupID) //nolint:golint,contextcheck
		}
	}
	if p.TS2DynamicTalkgroupID != nil {
		m.subscriptionsMutex.RLock()
		_, ok := m.subscriptions[p.RadioID][*p.TS2DynamicTalkgroupID]
		m.subscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.subscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID].Lock()
			m.subscriptions[p.RadioID][*p.TS2DynamicTalkgroupID] = cancel
			m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID].Unlock()
			m.subscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, *p.TS2DynamicTalkgroupID) //nolint:golint,contextcheck
		}
	}
}

func (m *SubscriptionManager) subscribeRepeater(ctx context.Context, redis *redis.Client, p models.Repeater) {
	if config.GetConfig().Debug {
		klog.Infof("Listening for calls on repeater %d", p.RadioID)
	}
	pubsub := redis.Subscribe(ctx, fmt.Sprintf("hbrp:packets:repeater:%d", p.RadioID))
	defer func() {
		err := pubsub.Unsubscribe(ctx, fmt.Sprintf("hbrp:packets:repeater:%d", p.RadioID))
		if err != nil {
			klog.Errorf("Error unsubscribing from hbrp:packets:repeater:%d: %s", p.RadioID, err)
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
				klog.Infof("Context canceled, stopping subscription to hbrp:packets:repeater:%d", p.RadioID)
			}
			m.subscriptionsMutex.Lock()
			_, ok := m.subscriptionCancelMutex[p.RadioID][p.RadioID]
			if ok {
				m.subscriptionCancelMutex[p.RadioID][p.RadioID].Lock()
			}
			delete(m.subscriptions[p.RadioID], p.RadioID)
			if ok {
				m.subscriptionCancelMutex[p.RadioID][p.RadioID].Unlock()
				delete(m.subscriptionCancelMutex[p.RadioID], p.RadioID)
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
			// This packet is already for us and we don't want to modify the slot
			packet := models.UnpackPacket(rawPacket.Data)
			packet.Repeater = p.RadioID
			redis.Publish(ctx, "hbrp:outgoing:noaddr", packet.Encode())
		}
	}
}

func (m *SubscriptionManager) subscribeTG(ctx context.Context, redis *redis.Client, p models.Repeater, tg uint) {
	if tg == 0 {
		return
	}
	if config.GetConfig().Debug {
		klog.Infof("Listening for calls on repeater %d, talkgroup %d", p.RadioID, tg)
	}
	pubsub := redis.Subscribe(ctx, fmt.Sprintf("hbrp:packets:talkgroup:%d", tg))
	defer func() {
		err := pubsub.Unsubscribe(ctx, fmt.Sprintf("hbrp:packets:talkgroup:%d", tg))
		if err != nil {
			klog.Errorf("Error unsubscribing from hbrp:packets:talkgroup:%d: %s", tg, err)
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
				klog.Infof("Context canceled, stopping subscription to hbrp:packets:repeater:%d, talkgroup %d", p.RadioID, tg)
			}
			m.subscriptionsMutex.Lock()
			_, ok := m.subscriptionCancelMutex[p.RadioID][tg]
			if ok {
				m.subscriptionCancelMutex[p.RadioID][tg].Lock()
			}
			delete(m.subscriptions[p.RadioID], tg)
			if ok {
				m.subscriptionCancelMutex[p.RadioID][tg].Unlock()
				delete(m.subscriptionCancelMutex[p.RadioID], tg)
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
			packet := models.UnpackPacket(rawPacket.Data)

			if packet.Repeater == p.RadioID {
				continue
			}

			want, slot := p.WantRX(packet)
			if want {
				// This packet is for the repeater's dynamic talkgroup
				// We need to send it to the repeater
				packet.Repeater = p.RadioID
				packet.Slot = slot
				redis.Publish(ctx, "hbrp:outgoing:noaddr", packet.Encode())
			} else {
				// We're subscribed but don't want this packet? With a talkgroup that can only mean we're unlinked, so we should unsubscribe
				err := pubsub.Unsubscribe(ctx, fmt.Sprintf("hbrp:packets:talkgroup:%d", tg))
				if err != nil {
					klog.Errorf("Error unsubscribing from packets:talkgroup:%d: %s", tg, err)
				}
				err = pubsub.Close()
				if err != nil {
					klog.Errorf("Error closing pubsub connection: %s", err)
				}
				return
			}
		}
	}
}
