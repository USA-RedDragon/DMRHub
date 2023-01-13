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
	RadioID                uint           `msg:"radio_id" gorm:"primaryKey"`
	Connection             string         `msg:"connection"`
	Connected              time.Time      `msg:"connected"`
	PingsReceived          int            `msg:"pings_received"`
	LastPing               time.Time      `msg:"last_ping"`
	IP                     string         `msg:"ip"`
	Port                   int            `msg:"port"`
	Salt                   uint32         `msg:"salt"`
	Callsign               string         `msg:"callsign"`
	RXFrequency            int            `msg:"rx_frequency"`
	TXFrequency            int            `msg:"tx_frequency"`
	TXPower                int            `msg:"tx_power"`
	ColorCode              int            `msg:"color_code"`
	Latitude               float32        `msg:"latitude"`
	Longitude              float32        `msg:"longitude"`
	Height                 int            `msg:"height"`
	Location               string         `msg:"location"`
	Description            string         `msg:"description"`
	Slots                  int            `msg:"slots"`
	URL                    string         `msg:"url"`
	SoftwareID             string         `msg:"software_id"`
	PackageID              string         `msg:"package_id"`
	Password               string         `msg:"-" json:"-"`
	TS1StaticTalkgroups    []Talkgroup    `msg:"-" gorm:"many2many:repeater_ts1_static_talkgroups;"`
	TS2StaticTalkgroups    []Talkgroup    `msg:"-" gorm:"many2many:repeater_ts2_static_talkgroups;"`
	TS1TemporaryTalkgroups []Talkgroup    `msg:"-" gorm:"many2many:repeater_ts1_temporary_talkgroups;"`
	TS2TemporaryTalkgroups []Talkgroup    `msg:"-" gorm:"many2many:repeater_ts2_temporary_talkgroups;"`
	CreatedAt              time.Time      `msg:"-" json:"-"`
	UpdatedAt              time.Time      `msg:"-" json:"-"`
	DeletedAt              gorm.DeletedAt `gorm:"index" msg:"-" json:"-"`
}

func MakeRepeater(radioId uint, salt uint32, socketAddr net.UDPAddr) Repeater {
	return Repeater{
		Connection:             "DISCONNECTED",
		Connected:              time.UnixMilli(0),
		PingsReceived:          0,
		LastPing:               time.UnixMilli(0),
		IP:                     socketAddr.IP.String(),
		Port:                   socketAddr.Port,
		Salt:                   salt,
		RadioID:                radioId,
		Callsign:               "",
		RXFrequency:            0,
		TXFrequency:            0,
		TXPower:                0,
		ColorCode:              0,
		Latitude:               0,
		Longitude:              0,
		Height:                 0,
		Location:               "",
		Description:            "",
		Slots:                  0,
		URL:                    "",
		SoftwareID:             "",
		PackageID:              "",
		TS1StaticTalkgroups:    []Talkgroup{},
		TS2StaticTalkgroups:    []Talkgroup{},
		TS1TemporaryTalkgroups: []Talkgroup{},
		TS2TemporaryTalkgroups: []Talkgroup{},
		Password:               "",
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
