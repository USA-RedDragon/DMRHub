package main

import (
	"fmt"
	"net"
	"time"
)

//go:generate msgp
type HomeBrewProtocolPeer struct {
	Connection    string    `msg:"connection"`
	Connected     time.Time `msg:"connected"`
	PingsReceived int       `msg:"pings_received"`
	LastPing      time.Time `msg:"last_ping"`
	IP            string    `msg:"ip"`
	Port          int       `msg:"port"`
	Salt          uint32    `msg:"salt"`
	RadioID       int       `msg:"radio_id"`
	Callsign      string    `msg:"callsign"`
	RXFrequency   int       `msg:"rx_frequency"`
	TXFrequency   int       `msg:"tx_frequency"`
	TXPower       int       `msg:"tx_power"`
	ColorCode     int       `msg:"color_code"`
	Latitude      float32   `msg:"latitude"`
	Longitude     float32   `msg:"longitude"`
	Height        int       `msg:"height"`
	Location      string    `msg:"location"`
	Description   string    `msg:"description"`
	Slots         int       `msg:"slots"`
	URL           string    `msg:"url"`
	SoftwareID    string    `msg:"software_id"`
	PackageID     string    `msg:"package_id"`
}

func makePeer(radioId int, salt uint32, socketAddr net.UDPAddr) HomeBrewProtocolPeer {
	return HomeBrewProtocolPeer{
		Connection:    "DISCONNECTED",
		Connected:     time.UnixMilli(0),
		PingsReceived: 0,
		LastPing:      time.UnixMilli(0),
		IP:            socketAddr.IP.String(),
		Port:          socketAddr.Port,
		Salt:          salt,
		RadioID:       radioId,
		Callsign:      "",
		RXFrequency:   0,
		TXFrequency:   0,
		TXPower:       0,
		ColorCode:     0,
		Latitude:      0,
		Longitude:     0,
		Height:        0,
		Location:      "",
		Description:   "",
		Slots:         0,
		URL:           "",
		SoftwareID:    "",
		PackageID:     "",
	}
}

func (p HomeBrewProtocolPeer) String() string {
	// Create a JSON string of the peer
	return fmt.Sprintf(`{
	"Connection": "%s",
	"Connected": "%s",
	"PingsReceived": %d,
	"LastPing": "%s",
	"IP": "%s",
	"Port": %d,
	"Salt": %d,
	"RadioID": %d,
	"Callsign": "%s",
	"RXFrequency": %d,
	"TXFrequency": %d,
	"TXPower": %d,
	"ColorCode": %d,
	"Latitude": %f,
	"Longitude": %f,
	"Height": %d,
	"Location": "%s",
	"Description": "%s",
	"Slots": %d,
	"URL": "%s",
	"SoftwareID": "%s",
	"PackageID": "%s"
}`,
		p.Connection,
		p.Connected,
		p.PingsReceived,
		p.LastPing,
		p.IP,
		p.Port,
		p.Salt,
		p.RadioID,
		p.Callsign,
		p.RXFrequency,
		p.TXFrequency,
		p.TXPower,
		p.ColorCode,
		p.Latitude,
		p.Longitude,
		p.Height,
		p.Location,
		p.Description,
		p.Slots,
		p.URL,
		p.SoftwareID,
		p.PackageID,
	)
}
