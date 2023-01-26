package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis"
	"gorm.io/gorm"
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
	TS1DynamicTalkgroupID uint           `json:"-" msg:"-"`
	TS2DynamicTalkgroupID uint           `json:"-" msg:"-"`
	TS1DynamicTalkgroup   Talkgroup      `json:"ts1_dynamic_talkgroup" gorm:"foreignKey:TS1DynamicTalkgroupID" msg:"-"`
	TS2DynamicTalkgroup   Talkgroup      `json:"ts2_dynamic_talkgroup" gorm:"foreignKey:TS2DynamicTalkgroupID" msg:"-"`
	Owner                 User           `json:"owner" gorm:"foreignKey:OwnerID" msg:"-"`
	OwnerID               uint           `json:"-" msg:"-"`
	Hotspot               bool           `json:"hotspot" msg:"hotspot"`
	CreatedAt             time.Time      `json:"created_at" msg:"-"`
	UpdatedAt             time.Time      `json:"-" msg:"-"`
	DeletedAt             gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

func (p Repeater) ListenForCalls(redisHost string) {
	klog.Infof("Listening for calls on repeater %d", p.RadioID)
	redis := redis.NewClient(&redis.Options{
		Addr: redisHost,
	})
	// Subscribe to Redis "packets:repeater:<id>" channel for a dmr.RawDMRPacket
	// This channel is used to get private calls headed to this repeater
	// When a packet is received, we need to publish it to "outgoing" channel
	// with the destination repeater ID as this one
	go p.subscribeRepeater(redis)

	// Subscribe to Redis "packets:talkgroup:<id>" channel for each talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		go p.subscribeTG(redis, tg.ID)
	}
	for _, tg := range p.TS2StaticTalkgroups {
		go p.subscribeTG(redis, tg.ID)
	}
	go p.subscribeTG(redis, p.TS1DynamicTalkgroup.ID)
	go p.subscribeTG(redis, p.TS2DynamicTalkgroup.ID)
}

func (p *Repeater) subscribeRepeater(redis *redis.Client) {
	pubsub := redis.Subscribe(fmt.Sprintf("packets:repeater:%d", p.RadioID))
	defer pubsub.Close()
	for {
		msg, err := pubsub.ReceiveMessage()
		if err != nil {
			klog.Errorf("Failed to receive message from Redis: %s", err)
			return
		}
		klog.Error("PUBSUB: Received packet for repeater", p.RadioID)
		rawPacket := RawDMRPacket{}
		_, err = rawPacket.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			klog.Errorf("Failed to unmarshal raw packet: %s", err)
			return
		}
		// This packet is already for us and we don't want to modify the slot
		packet := UnpackPacket(rawPacket.Data)
		packet.Repeater = p.RadioID
		redis.Publish("outgoing:noaddr", packet.Encode())
	}
}

func (p *Repeater) subscribeTG(redis *redis.Client, tg uint) {
	pubsub := redis.Subscribe(fmt.Sprintf("packets:talkgroup:%d", tg))
	defer pubsub.Close()
	for {
		msg, err := pubsub.ReceiveMessage()
		if err != nil {
			klog.Errorf("Failed to receive message from Redis: %s", err)
			return
		}
		klog.Errorf("PUBSUB: Received packet for talkgroup %d on repeater %d", tg, p.RadioID)
		rawPacket := RawDMRPacket{}
		_, err = rawPacket.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			klog.Errorf("Failed to unmarshal raw packet: %s", err)
			return
		}
		packet := UnpackPacket(rawPacket.Data)
		want, slot := p.WantRX(packet)
		if want && packet.Src != p.RadioID {
			// This packet is for the repeater's dynamic talkgroup
			// We need to send it to the repeater
			packet.Repeater = p.RadioID
			packet.Slot = slot
			redis.Publish("outgoing:noaddr", packet.Encode())
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
	db.Preload("Owner").Preload("TS1DynamicTalkgroup").Preload("TS2DynamicTalkgroup").Preload("TS1StaticTalkgroups").Preload("TS2StaticTalkgroups").Find(&repeaters)
	return repeaters
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

func (p *Repeater) WantRX(packet Packet) (bool, bool) {
	want := false
	slot := false

	switch packet.Dst {
	case p.TS2DynamicTalkgroupID:
		want = true
		slot = true
	case p.TS1DynamicTalkgroupID:
		want = true
		slot = false
	case p.OwnerID:
		want = true
		slot = packet.Slot
	default:
		if p.InTS2StaticTalkgroups(packet.Dst) {
			want = true
			slot = true
		} else if p.InTS1StaticTalkgroups(packet.Dst) {
			want = true
			slot = false
		}
	}

	return want, slot
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
