package models

//go:generate msgp
type Packet struct {
	Signature   string   `msg:"signature"`
	Seq         uint     `msg:"seq"`
	Src         uint     `msg:"src"`
	Dst         uint     `msg:"dst"`
	Repeater    uint     `msg:"repeater"`
	Slot        bool     `msg:"slot"`
	GroupCall   bool     `msg:"groupCall"`
	FrameType   uint     `msg:"frameType"`
	DTypeOrVSeq uint     `msg:"dtypeOrVSeq"`
	StreamId    uint     `msg:"streamId"`
	DMRData     [33]byte `msg:"dmrData"`
}

func UnpackPacket(data []byte) Packet {
	var packet Packet
	packet.Signature = string(data[:4])
	packet.Seq = uint(data[4])
	packet.Src = uint(data[5])<<16 | uint(data[6])<<8 | uint(data[7])
	packet.Dst = uint(data[8])<<16 | uint(data[9])<<8 | uint(data[10])
	packet.Repeater = uint(data[11])<<24 | uint(data[12])<<16 | uint(data[13])<<8 | uint(data[14])
	bits := data[15]
	packet.Slot = false
	if (bits & 0x80) != 0 {
		packet.Slot = true
	}
	packet.GroupCall = true
	if (bits & 0x40) != 0 {
		packet.GroupCall = false
	}
	packet.FrameType = uint((bits & 0x30) >> 4)
	packet.DTypeOrVSeq = uint(bits & 0xF)
	packet.StreamId = uint(data[16])<<24 | uint(data[17])<<16 | uint(data[18])<<8 | uint(data[19])
	copy(packet.DMRData[:], data[20:53])
	return packet
}

func (p Packet) Encode() []byte {
	// Encode the packet as we decoded
	data := make([]byte, 53)
	copy(data[:4], []byte(p.Signature))
	data[4] = byte(p.Seq)
	data[5] = byte(p.Src >> 16)
	data[6] = byte(p.Src >> 8)
	data[7] = byte(p.Src)
	data[8] = byte(p.Dst >> 16)
	data[9] = byte(p.Dst >> 8)
	data[10] = byte(p.Dst)
	data[11] = byte(p.Repeater >> 24)
	data[12] = byte(p.Repeater >> 16)
	data[13] = byte(p.Repeater >> 8)
	data[14] = byte(p.Repeater)
	bits := byte(0)
	if p.Slot {
		bits |= 0x80
	}
	if !p.GroupCall {
		bits |= 0x40
	}
	bits |= byte(p.FrameType << 4)
	bits |= byte(p.DTypeOrVSeq)
	data[15] = bits
	data[16] = byte(p.StreamId >> 24)
	data[17] = byte(p.StreamId >> 16)
	data[18] = byte(p.StreamId >> 8)
	data[19] = byte(p.StreamId)
	copy(data[20:53], p.DMRData[:])
	return data
}
