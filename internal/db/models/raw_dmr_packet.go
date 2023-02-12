package models

// RawDMRPacket is a raw DMR packet
//
//go:generate msgp
type RawDMRPacket struct {
	Data       []byte `msg:"data"`
	RemoteIP   string `msg:"remote_ip"`
	RemotePort int    `msg:"remote_port"`
}
