package models

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/klog/v2"
)

//go:generate msgp
type Repeater struct {
	RadioID               uint           `json:"id" gorm:"primaryKey" msg:"radio_id"`
	Connection            string         `json:"-" gorm:"-" msg:"connection"`
	Connected             time.Time      `json:"connected_time" msg:"connected"`
	PingsReceived         uint           `json:"-" gorm:"-" msg:"pings_received"`
	LastPing              time.Time      `json:"last_ping_time" msg:"last_ping"`
	IP                    string         `json:"-" gorm:"-" msg:"ip"`
	Port                  int            `json:"-" gorm:"-" msg:"port"`
	Salt                  uint32         `json:"-" gorm:"-" msg:"salt"`
	Callsign              string         `json:"callsign" msg:"callsign"`
	RXFrequency           uint           `json:"rx_frequency" msg:"rx_frequency"`
	TXFrequency           uint           `json:"tx_frequency" msg:"tx_frequency"`
	TXPower               uint           `json:"tx_power" msg:"tx_power"`
	ColorCode             uint           `json:"color_code" msg:"color_code"`
	Latitude              float32        `json:"latitude" msg:"latitude"`
	Longitude             float32        `json:"longitude" msg:"longitude"`
	Height                int            `json:"height" msg:"height"`
	Location              string         `json:"location" msg:"location"`
	Description           string         `json:"description" msg:"description"`
	Slots                 uint           `json:"slots" msg:"slots"`
	URL                   string         `json:"url" msg:"url"`
	SoftwareID            string         `json:"software_id" msg:"software_id"`
	PackageID             string         `json:"package_id" msg:"package_id"`
	Password              string         `json:"-" msg:"-"`
	TS1StaticTalkgroups   []Talkgroup    `json:"ts1_static_talkgroups" gorm:"many2many:repeater_ts1_static_talkgroups;" msg:"-"`
	TS2StaticTalkgroups   []Talkgroup    `json:"ts2_static_talkgroups" gorm:"many2many:repeater_ts2_static_talkgroups;" msg:"-"`
	TS1DynamicTalkgroupID *uint          `json:"-" msg:"-"`
	TS2DynamicTalkgroupID *uint          `json:"-" msg:"-"`
	TS1DynamicTalkgroup   Talkgroup      `json:"ts1_dynamic_talkgroup" gorm:"foreignKey:TS1DynamicTalkgroupID" msg:"-"`
	TS2DynamicTalkgroup   Talkgroup      `json:"ts2_dynamic_talkgroup" gorm:"foreignKey:TS2DynamicTalkgroupID" msg:"-"`
	Owner                 User           `json:"owner" gorm:"foreignKey:OwnerID" msg:"-"`
	OwnerID               uint           `json:"-" msg:"-"`
	Hotspot               bool           `json:"hotspot" msg:"hotspot"`
	CreatedAt             time.Time      `json:"created_at" msg:"-"`
	UpdatedAt             time.Time      `json:"-" msg:"-"`
	DeletedAt             gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

var talkgroupSubscriptions = make(map[uint]map[uint]context.CancelFunc)
var talkgroupSubscriptionsMutex = &sync.RWMutex{}
var subscriptionCancelMutex = make(map[uint]map[uint]*sync.RWMutex)

func (p Repeater) CancelSubscription(talkgroupID uint) {
	talkgroupSubscriptionsMutex.RLock()
	subscriptionCancelMutex[p.RadioID][talkgroupID].RLock()
	cancel, ok := talkgroupSubscriptions[p.RadioID][talkgroupID]
	subscriptionCancelMutex[p.RadioID][talkgroupID].RUnlock()
	talkgroupSubscriptionsMutex.RUnlock()
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
		talkgroupSubscriptionsMutex.Lock()
		subscriptionCancelMutex[p.RadioID][talkgroupID].Lock()
		delete(talkgroupSubscriptions[p.RadioID], talkgroupID)
		subscriptionCancelMutex[p.RadioID][talkgroupID].Unlock()
		delete(subscriptionCancelMutex[p.RadioID], talkgroupID)
		talkgroupSubscriptionsMutex.Unlock()
		cancel()
	}
}

func (p Repeater) CancelAllSubscriptions() {
	if config.GetConfig().Debug {
		klog.Errorf("Cancelling all newly inactive subscriptions for repeater %d", p.RadioID)
	}
	talkgroupSubscriptionsMutex.RLock()
	for tgID := range talkgroupSubscriptions[p.RadioID] {
		talkgroupSubscriptionsMutex.RUnlock()
		p.CancelSubscription(tgID)
		talkgroupSubscriptionsMutex.RLock()
	}
	talkgroupSubscriptionsMutex.RUnlock()
}

func (p Repeater) ListenForCallsOn(ctx context.Context, redis *redis.Client, talkgroupID uint) {
	talkgroupSubscriptionsMutex.RLock()
	_, ok := talkgroupSubscriptions[p.RadioID][talkgroupID]
	talkgroupSubscriptionsMutex.RUnlock()
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		talkgroupSubscriptionsMutex.Lock()
		_, ok = subscriptionCancelMutex[p.RadioID][talkgroupID]
		if !ok {
			subscriptionCancelMutex[p.RadioID][talkgroupID] = &sync.RWMutex{}
		}
		subscriptionCancelMutex[p.RadioID][talkgroupID].Lock()
		talkgroupSubscriptions[p.RadioID][talkgroupID] = cancel
		subscriptionCancelMutex[p.RadioID][talkgroupID].Unlock()
		talkgroupSubscriptionsMutex.Unlock()
		go p.subscribeTG(newCtx, redis, talkgroupID)
	}
}

func (p Repeater) ListenForCalls(ctx context.Context, redis *redis.Client) {
	// Subscribe to Redis "packets:repeater:<id>" channel for a dmr.RawDMRPacket
	// This channel is used to get private calls headed to this repeater
	// When a packet is received, we need to publish it to "outgoing" channel
	// with the destination repeater ID as this one
	talkgroupSubscriptionsMutex.RLock()
	_, ok := talkgroupSubscriptions[p.RadioID]
	talkgroupSubscriptionsMutex.RUnlock()
	if !ok {
		talkgroupSubscriptionsMutex.Lock()
		talkgroupSubscriptions[p.RadioID] = make(map[uint]context.CancelFunc)
		subscriptionCancelMutex[p.RadioID] = make(map[uint]*sync.RWMutex)
		talkgroupSubscriptionsMutex.Unlock()
	}
	talkgroupSubscriptionsMutex.RLock()
	_, ok = talkgroupSubscriptions[p.RadioID][p.RadioID]
	talkgroupSubscriptionsMutex.RUnlock()
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		talkgroupSubscriptionsMutex.Lock()
		_, ok = subscriptionCancelMutex[p.RadioID][p.RadioID]
		if !ok {
			subscriptionCancelMutex[p.RadioID][p.RadioID] = &sync.RWMutex{}
		}
		subscriptionCancelMutex[p.RadioID][p.RadioID].Lock()
		talkgroupSubscriptions[p.RadioID][p.RadioID] = cancel
		subscriptionCancelMutex[p.RadioID][p.RadioID].Unlock()
		talkgroupSubscriptionsMutex.Unlock()
		go p.subscribeRepeater(newCtx, redis)
	}

	// Subscribe to Redis "packets:talkgroup:<id>" channel for each talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		talkgroupSubscriptionsMutex.RLock()
		_, ok := talkgroupSubscriptions[p.RadioID][tg.ID]
		talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			talkgroupSubscriptionsMutex.Lock()
			_, ok = subscriptionCancelMutex[p.RadioID][tg.ID]
			if !ok {
				subscriptionCancelMutex[p.RadioID][tg.ID] = &sync.RWMutex{}
			}
			subscriptionCancelMutex[p.RadioID][tg.ID].Lock()
			talkgroupSubscriptions[p.RadioID][tg.ID] = cancel
			subscriptionCancelMutex[p.RadioID][tg.ID].Unlock()
			talkgroupSubscriptionsMutex.Unlock()
			go p.subscribeTG(newCtx, redis, tg.ID)
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		talkgroupSubscriptionsMutex.RLock()
		_, ok := talkgroupSubscriptions[p.RadioID][tg.ID]
		talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			talkgroupSubscriptionsMutex.Lock()
			_, ok = subscriptionCancelMutex[p.RadioID][tg.ID]
			if !ok {
				subscriptionCancelMutex[p.RadioID][tg.ID] = &sync.RWMutex{}
			}
			subscriptionCancelMutex[p.RadioID][tg.ID].Lock()
			talkgroupSubscriptions[p.RadioID][tg.ID] = cancel
			subscriptionCancelMutex[p.RadioID][tg.ID].Unlock()
			talkgroupSubscriptionsMutex.Unlock()
			go p.subscribeTG(newCtx, redis, tg.ID)
		}
	}
	if p.TS1DynamicTalkgroupID != nil {
		talkgroupSubscriptionsMutex.RLock()
		_, ok := talkgroupSubscriptions[p.RadioID][*p.TS1DynamicTalkgroupID]
		talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			talkgroupSubscriptionsMutex.Lock()
			_, ok = subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID]
			if !ok {
				subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID] = &sync.RWMutex{}
			}
			subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID].Lock()
			talkgroupSubscriptions[p.RadioID][*p.TS1DynamicTalkgroupID] = cancel
			subscriptionCancelMutex[p.RadioID][*p.TS1DynamicTalkgroupID].Unlock()
			talkgroupSubscriptionsMutex.Unlock()
			go p.subscribeTG(newCtx, redis, *p.TS1DynamicTalkgroupID)
		}
	}
	if p.TS2DynamicTalkgroupID != nil {
		talkgroupSubscriptionsMutex.RLock()
		_, ok := talkgroupSubscriptions[p.RadioID][*p.TS2DynamicTalkgroupID]
		talkgroupSubscriptionsMutex.RUnlock()
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			talkgroupSubscriptionsMutex.Lock()
			_, ok = subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID]
			if !ok {
				subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID] = &sync.RWMutex{}
			}
			subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID].Lock()
			talkgroupSubscriptions[p.RadioID][*p.TS2DynamicTalkgroupID] = cancel
			subscriptionCancelMutex[p.RadioID][*p.TS2DynamicTalkgroupID].Unlock()
			talkgroupSubscriptionsMutex.Unlock()
			go p.subscribeTG(newCtx, redis, *p.TS2DynamicTalkgroupID)
		}
	}
}

func (p *Repeater) subscribeRepeater(ctx context.Context, redis *redis.Client) {
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
			talkgroupSubscriptionsMutex.Lock()
			_, ok := subscriptionCancelMutex[p.RadioID][p.RadioID]
			if ok {
				subscriptionCancelMutex[p.RadioID][p.RadioID].Lock()
			}
			delete(talkgroupSubscriptions[p.RadioID], p.RadioID)
			if ok {
				subscriptionCancelMutex[p.RadioID][p.RadioID].Unlock()
				delete(subscriptionCancelMutex[p.RadioID], p.RadioID)
			}
			talkgroupSubscriptionsMutex.Unlock()
			return
		case msg := <-pubsubChannel:
			rawPacket := RawDMRPacket{}
			_, err := rawPacket.UnmarshalMsg([]byte(msg.Payload))
			if err != nil {
				klog.Errorf("Failed to unmarshal raw packet: %s", err)
				continue
			}
			// This packet is already for us and we don't want to modify the slot
			packet := UnpackPacket(rawPacket.Data)
			packet.Repeater = p.RadioID
			redis.Publish(ctx, "outgoing:noaddr", packet.Encode())
		}
	}
}

func (p *Repeater) subscribeTG(ctx context.Context, redis *redis.Client, tg uint) {
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
			talkgroupSubscriptionsMutex.Lock()
			_, ok := subscriptionCancelMutex[p.RadioID][tg]
			if ok {
				subscriptionCancelMutex[p.RadioID][tg].Lock()
			}
			delete(talkgroupSubscriptions[p.RadioID], tg)
			if ok {
				subscriptionCancelMutex[p.RadioID][tg].Unlock()
				delete(subscriptionCancelMutex[p.RadioID], tg)
			}
			talkgroupSubscriptionsMutex.Unlock()
			return
		case msg := <-pubsubChannel:
			rawPacket := RawDMRPacket{}
			_, err := rawPacket.UnmarshalMsg([]byte(msg.Payload))
			if err != nil {
				klog.Errorf("Failed to unmarshal raw packet: %s", err)
				continue
			}
			packet := UnpackPacket(rawPacket.Data)
			if packet.Src == p.RadioID {
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

func (p *Repeater) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		klog.Errorf("Failed to marshal repeater to json: %s", err)
		return ""
	}
	return string(jsn)
}

func ListRepeaters(db *gorm.DB) []Repeater {
	var repeaters []Repeater
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Order("radio_id asc").Find(&repeaters)
	return repeaters
}

func CountRepeaters(db *gorm.DB) int {
	var count int64
	db.Model(&Repeater{}).Count(&count)
	return int(count)
}

func GetUserRepeaters(db *gorm.DB, ID uint) []Repeater {
	var repeaters []Repeater
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Where("owner_id = ?", ID).Order("radio_id asc").Find(&repeaters)
	return repeaters
}

func CountUserRepeaters(db *gorm.DB, ID uint) int {
	var count int64
	db.Model(&Repeater{}).Where("owner_id = ?", ID).Count(&count)
	return int(count)
}

func FindRepeaterByID(db *gorm.DB, ID uint) Repeater {
	var repeater Repeater
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").First(&repeater, ID)
	return repeater
}

func RepeaterExists(db *gorm.DB, repeater Repeater) bool {
	var count int64
	db.Model(&Repeater{}).Where("radio_id = ?", repeater.RadioID).Limit(1).Count(&count)
	return count > 0
}

func RepeaterIDExists(db *gorm.DB, id uint) bool {
	var count int64
	db.Model(&Repeater{}).Where("radio_id = ?", id).Limit(1).Count(&count)
	return count > 0
}

func DeleteRepeater(db *gorm.DB, id uint) {
	db.Unscoped().Select(clause.Associations, "TS1StaticTalkgroups").Select(clause.Associations, "TS2StaticTalkgroups").Delete(&Repeater{RadioID: id})
}

func (p *Repeater) WantRX(packet Packet) (bool, bool) {
	if packet.Dst == p.RadioID {
		return true, packet.Slot
	}

	if p.TS2DynamicTalkgroupID != nil {
		if packet.Dst == *p.TS2DynamicTalkgroupID {
			return true, true
		}
	}

	if p.TS1DynamicTalkgroupID != nil {
		if packet.Dst == *p.TS1DynamicTalkgroupID {
			return true, false
		}
	}

	if p.InTS2StaticTalkgroups(packet.Dst) {
		return true, true
	} else if p.InTS1StaticTalkgroups(packet.Dst) {
		return true, false
	}

	return false, false
}

func (p *Repeater) InTS2StaticTalkgroups(dest uint) bool {
	for _, tg := range p.TS2StaticTalkgroups {
		if dest == tg.ID {
			return true
		}
	}
	return false
}

func (p *Repeater) InTS1StaticTalkgroups(dest uint) bool {
	for _, tg := range p.TS1StaticTalkgroups {
		if dest == tg.ID {
			return true
		}
	}
	return false
}
