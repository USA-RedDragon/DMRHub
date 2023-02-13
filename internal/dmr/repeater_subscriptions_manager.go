package dmr

import (
	"context"
	"fmt"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
	"k8s.io/klog/v2"
)

var repeaterSubscriptionManager *RepeaterSubscriptionManager //nolint:golint,gochecknoglobals

type RepeaterSubscriptionManager struct {
	talkgroupSubscriptions      map[uint]map[uint]context.CancelFunc
	talkgroupSubscriptionsMutex *sync.RWMutex
	subscriptionCancelMutex     map[uint]map[uint]*sync.RWMutex
}

func GetRepeaterSubscriptionManager() *RepeaterSubscriptionManager {
	if repeaterSubscriptionManager == nil {
		repeaterSubscriptionManager = &RepeaterSubscriptionManager{
			talkgroupSubscriptions:      make(map[uint]map[uint]context.CancelFunc),
			talkgroupSubscriptionsMutex: &sync.RWMutex{},
			subscriptionCancelMutex:     make(map[uint]map[uint]*sync.RWMutex),
		}
	}
	return repeaterSubscriptionManager
}

func (m *RepeaterSubscriptionManager) CancelSubscription(p models.Repeater, talkgroupID uint) {
	m.talkgroupSubscriptionsMutex.RLock()
	m.subscriptionCancelMutex[p.RadioID][talkgroupID].RLock()
	cancel, ok := m.talkgroupSubscriptions[p.RadioID][talkgroupID]
	m.subscriptionCancelMutex[p.RadioID][talkgroupID].RUnlock()
	m.talkgroupSubscriptionsMutex.RUnlock()
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
		m.talkgroupSubscriptionsMutex.Lock()
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Lock()
		delete(m.talkgroupSubscriptions[p.RadioID], talkgroupID)
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Unlock()
		delete(m.subscriptionCancelMutex[p.RadioID], talkgroupID)
		m.talkgroupSubscriptionsMutex.Unlock()
		cancel()
	}
}

func (m *RepeaterSubscriptionManager) CancelAllSubscriptions(p models.Repeater) {
	if config.GetConfig().Debug {
		klog.Errorf("Cancelling all newly inactive subscriptions for repeater %d", p.RadioID)
	}
	m.talkgroupSubscriptionsMutex.RLock()
	for tgID := range m.talkgroupSubscriptions[p.RadioID] {
		m.talkgroupSubscriptionsMutex.RUnlock()
		m.CancelSubscription(p, tgID)
		m.talkgroupSubscriptionsMutex.RLock()
	}
	m.talkgroupSubscriptionsMutex.RUnlock()
}

func (m *RepeaterSubscriptionManager) ListenForCallsOn(ctx context.Context, redis *redis.Client, p models.Repeater, talkgroupID uint) {
	m.talkgroupSubscriptionsMutex.RLock()
	_, ok := m.talkgroupSubscriptions[p.RadioID][talkgroupID]
	m.talkgroupSubscriptionsMutex.RUnlock()
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		m.talkgroupSubscriptionsMutex.Lock()
		_, ok = m.subscriptionCancelMutex[p.RadioID][talkgroupID]
		if !ok {
			m.subscriptionCancelMutex[p.RadioID][talkgroupID] = &sync.RWMutex{}
		}
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Lock()
		m.talkgroupSubscriptions[p.RadioID][talkgroupID] = cancel
		m.subscriptionCancelMutex[p.RadioID][talkgroupID].Unlock()
		m.talkgroupSubscriptionsMutex.Unlock()
		go m.subscribeTG(newCtx, redis, p, talkgroupID)
	}
}

func (m *RepeaterSubscriptionManager) ListenForCalls(ctx context.Context, redis *redis.Client, p models.Repeater) {
	// Subscribe to Redis "packets:repeater:<id>" channel for a dmr.RawDMRPacket
	// This channel is used to get private calls headed to this repeater
	// When a packet is received, we need to publish it to "outgoing" channel
	// with the destination repeater ID as this one
	m.talkgroupSubscriptionsMutex.RLock()
	_, ok := m.talkgroupSubscriptions[p.RadioID]
	m.talkgroupSubscriptionsMutex.RUnlock()
	if !ok {
		m.talkgroupSubscriptionsMutex.Lock()
		m.talkgroupSubscriptions[p.RadioID] = make(map[uint]context.CancelFunc)
		m.subscriptionCancelMutex[p.RadioID] = make(map[uint]*sync.RWMutex)
		m.talkgroupSubscriptionsMutex.Unlock()
	}
	m.talkgroupSubscriptionsMutex.RLock()
	_, ok = m.talkgroupSubscriptions[p.RadioID][p.RadioID]
	m.talkgroupSubscriptionsMutex.RUnlock()
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		m.talkgroupSubscriptionsMutex.Lock()
		_, ok = m.subscriptionCancelMutex[p.RadioID][p.RadioID]
		if !ok {
			m.subscriptionCancelMutex[p.RadioID][p.RadioID] = &sync.RWMutex{}
		}
		m.subscriptionCancelMutex[p.RadioID][p.RadioID].Lock()
		m.talkgroupSubscriptions[p.RadioID][p.RadioID] = cancel
		m.subscriptionCancelMutex[p.RadioID][p.RadioID].Unlock()
		m.talkgroupSubscriptionsMutex.Unlock()
		go m.subscribeRepeater(newCtx, redis, p)
	}

	// Subscribe to Redis "packets:talkgroup:<id>" channel for each talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		m.talkgroupSubscriptionsMutex.RLock()
		_, ok := m.talkgroupSubscriptions[p.RadioID][tg.ID]
		m.talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.talkgroupSubscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][tg.ID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][tg.ID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Lock()
			m.talkgroupSubscriptions[p.RadioID][tg.ID] = cancel
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Unlock()
			m.talkgroupSubscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, tg.ID)
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		m.talkgroupSubscriptionsMutex.RLock()
		_, ok := m.talkgroupSubscriptions[p.RadioID][tg.ID]
		m.talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.talkgroupSubscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][tg.ID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][tg.ID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Lock()
			m.talkgroupSubscriptions[p.RadioID][tg.ID] = cancel
			m.subscriptionCancelMutex[p.RadioID][tg.ID].Unlock()
			m.talkgroupSubscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, tg.ID)
		}
	}
	if p.TS1DynamicTalkgroupID != nil {
		m.talkgroupSubscriptionsMutex.RLock()
		_, ok := m.talkgroupSubscriptions[p.RadioID][*p.TS1DynamicTalkgroupID]
		m.talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.talkgroupSubscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID].Lock()
			m.talkgroupSubscriptions[p.RadioID][*p.TS1DynamicTalkgroupID] = cancel
			m.subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID].Unlock()
			m.talkgroupSubscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, *p.TS1DynamicTalkgroupID)
		}
	}
	if p.TS2DynamicTalkgroupID != nil {
		m.talkgroupSubscriptionsMutex.RLock()
		_, ok := m.talkgroupSubscriptions[p.RadioID][*p.TS2DynamicTalkgroupID]
		m.talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			m.talkgroupSubscriptionsMutex.Lock()
			_, ok = m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID]
			if !ok {
				m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID] = &sync.RWMutex{}
			}
			m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID].Lock()
			m.talkgroupSubscriptions[p.RadioID][*p.TS2DynamicTalkgroupID] = cancel
			m.subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID].Unlock()
			m.talkgroupSubscriptionsMutex.Unlock()
			go m.subscribeTG(newCtx, redis, p, *p.TS2DynamicTalkgroupID)
		}
	}
}

func (m *RepeaterSubscriptionManager) subscribeRepeater(ctx context.Context, redis *redis.Client, p models.Repeater) {
	if config.GetConfig().Debug {
		klog.Infof("Listening for calls on repeater %d", p.RadioID)
	}
	pubsub := redis.Subscribe(ctx, fmt.Sprintf("packets:repeater:%d", p.RadioID))
	defer func() {
		err := pubsub.Unsubscribe(ctx, fmt.Sprintf("packets:repeater:%d", p.RadioID))
		if err != nil {
			klog.Errorf("Error unsubscribing from packets:repeater:%d: %s", p.RadioID, err)
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
				klog.Infof("Context canceled, stopping subscription to packets:repeater:%d", p.RadioID)
			}
			m.talkgroupSubscriptionsMutex.Lock()
			_, ok := m.subscriptionCancelMutex[p.RadioID][p.RadioID]
			if ok {
				m.subscriptionCancelMutex[p.RadioID][p.RadioID].Lock()
			}
			delete(m.talkgroupSubscriptions[p.RadioID], p.RadioID)
			if ok {
				m.subscriptionCancelMutex[p.RadioID][p.RadioID].Unlock()
				delete(m.subscriptionCancelMutex[p.RadioID], p.RadioID)
			}
			m.talkgroupSubscriptionsMutex.Unlock()
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
			redis.Publish(ctx, "outgoing:noaddr", packet.Encode())
		}
	}
}

func (m *RepeaterSubscriptionManager) subscribeTG(ctx context.Context, redis *redis.Client, p models.Repeater, tg uint) {
	if tg == 0 {
		return
	}
	if config.GetConfig().Debug {
		klog.Infof("Listening for calls on repeater %d, talkgroup %d", p.RadioID, tg)
	}
	pubsub := redis.Subscribe(ctx, fmt.Sprintf("packets:talkgroup:%d", tg))
	defer func() {
		err := pubsub.Unsubscribe(ctx, fmt.Sprintf("packets:talkgroup:%d", tg))
		if err != nil {
			klog.Errorf("Error unsubscribing from packets:talkgroup:%d: %s", tg, err)
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
				klog.Infof("Context canceled, stopping subscription to packets:repeater:%d, talkgroup %d", p.RadioID, tg)
			}
			m.talkgroupSubscriptionsMutex.Lock()
			_, ok := m.subscriptionCancelMutex[p.RadioID][tg]
			if ok {
				m.subscriptionCancelMutex[p.RadioID][tg].Lock()
			}
			delete(m.talkgroupSubscriptions[p.RadioID], tg)
			if ok {
				m.subscriptionCancelMutex[p.RadioID][tg].Unlock()
				delete(m.subscriptionCancelMutex[p.RadioID], tg)
			}
			m.talkgroupSubscriptionsMutex.Unlock()
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
				redis.Publish(ctx, "outgoing:noaddr", packet.Encode())
			} else {
				// We're subscribed but don't want this packet? With a talkgroup that can only mean we're unlinked, so we should unsubscribe
				err := pubsub.Unsubscribe(ctx, fmt.Sprintf("packets:talkgroup:%d", tg))
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
