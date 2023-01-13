package models

import (
	"encoding/json"
	"net"
	"time"

	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

//go:generate msgp
type Repeater struct {
	RadioID               uint           `msg:"radio_id" gorm:"primaryKey"`
	Connection            string         `msg:"connection" gorm:"-"`
	Connected             time.Time      `msg:"connected"`
	PingsReceived         int            `msg:"pings_received" gorm:"-"`
	LastPing              time.Time      `msg:"last_ping"`
	IP                    string         `msg:"ip"`
	Port                  int            `msg:"port"`
	Salt                  uint32         `msg:"salt" gorm:"-"`
	Callsign              string         `msg:"callsign" gorm:"uniqueIndex"`
	RXFrequency           int            `msg:"rx_frequency"`
	TXFrequency           int            `msg:"tx_frequency"`
	TXPower               int            `msg:"tx_power"`
	ColorCode             int            `msg:"color_code"`
	Latitude              float32        `msg:"latitude"`
	Longitude             float32        `msg:"longitude"`
	Height                int            `msg:"height"`
	Location              string         `msg:"location"`
	Description           string         `msg:"description"`
	Slots                 int            `msg:"slots"`
	URL                   string         `msg:"url"`
	SoftwareID            string         `msg:"software_id"`
	PackageID             string         `msg:"package_id"`
	Password              string         `msg:"-" json:"-"`
	TS1StaticTalkgroups   []Talkgroup    `msg:"-" gorm:"many2many:repeater_ts1_static_talkgroups;"`
	TS2StaticTalkgroups   []Talkgroup    `msg:"-" gorm:"many2many:repeater_ts2_static_talkgroups;"`
	TS1DynamicTalkgroupID uint           `msg:"-"`
	TS2DynamicTalkgroupID uint           `msg:"-"`
	TS1DynamicTalkgroup   Talkgroup      `msg:"-" gorm:"foreignKey:TS1DynamicTalkgroupID"`
	TS2DynamicTalkgroup   Talkgroup      `msg:"-" gorm:"foreignKey:TS2DynamicTalkgroupID"`
	Owner                 User           `msg:"-" json:"-" gorm:"foreignKey:OwnerID"`
	OwnerID               uint           `msg:"-"`
	SecureMode            bool           `msg:"-"`
	CreatedAt             time.Time      `msg:"-" json:"-"`
	UpdatedAt             time.Time      `msg:"-" json:"-"`
	DeletedAt             gorm.DeletedAt `gorm:"index" msg:"-" json:"-"`
}

func MakeRepeater(radioId uint, salt uint32, socketAddr net.UDPAddr) Repeater {
	return Repeater{
		Connection:            "DISCONNECTED",
		Connected:             time.UnixMilli(0),
		PingsReceived:         0,
		LastPing:              time.UnixMilli(0),
		IP:                    socketAddr.IP.String(),
		Port:                  socketAddr.Port,
		Salt:                  salt,
		RadioID:               radioId,
		Callsign:              "",
		RXFrequency:           0,
		TXFrequency:           0,
		TXPower:               0,
		ColorCode:             0,
		Latitude:              0,
		Longitude:             0,
		Height:                0,
		Location:              "",
		Description:           "",
		Slots:                 0,
		URL:                   "",
		SoftwareID:            "",
		PackageID:             "",
		TS1StaticTalkgroups:   []Talkgroup{},
		TS2StaticTalkgroups:   []Talkgroup{},
		TS1DynamicTalkgroupID: 0,
		TS2DynamicTalkgroupID: 0,
		Password:              "",
		Owner:                 User{},
		OwnerID:               0,
		SecureMode:            false,
	}
}

func (p Repeater) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		klog.Errorf("Failed to marshal repeater to json: %s", err)
		return ""
	}
	return string(jsn)
}

func FindRepeaterByID(db *gorm.DB, ID uint) Repeater {
	var repeater Repeater
	db.First(&repeater, ID)
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

func (p Repeater) WantRX(packet Packet) (bool, bool) {
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

func (p Repeater) InTS2StaticTalkgroups(dest uint) bool {
	for _, tg := range p.TS2StaticTalkgroups {
		if dest == tg.ID {
			return true
		}
	}
	return false
}

func (p Repeater) InTS1StaticTalkgroups(dest uint) bool {
	for _, tg := range p.TS1StaticTalkgroups {
		if dest == tg.ID {
			return true
		}
	}
	return false
}
