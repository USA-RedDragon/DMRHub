package dmr

import (
	"regexp"
)

type Command string

const (
	// Command Types
	COMMAND_DMRA    Command = "DMRA"
	COMMAND_DMRD    Command = "DMRD"    // DMR data
	COMMAND_MSTCL   Command = "MSTCL"   // master server is closing connection
	COMMAND_MSTNAK  Command = "MSTNAK"  // master -> repeater nak
	COMMAND_MSTPONG Command = "MSTPONG" // RPTPING response
	COMMAND_MSTN    Command = "MSTN"
	COMMAND_MSTP    Command = "MSTP"
	COMMAND_MSTC    Command = "MSTC"
	COMMAND_RPTL    Command = "RPTL"    // RPTLogin -- a repeater wants to login
	COMMAND_RPTPING Command = "RPTPING" // repeater -> master ping
	COMMAND_RPTCL   Command = "RPTCL"   // repeater wants to disconnect
	COMMAND_RPTACK  Command = "RPTACK"  // mater -> repeater ack
	COMMAND_RPTK    Command = "RPTK"    // Login challenge response
	COMMAND_RPTC    Command = "RPTC"    // repeater wants to send config or disconnect
	COMMAND_RPTP    Command = "RPTP"
	COMMAND_RPTA    Command = "RPTA"
	COMMAND_RPTO    Command = "RPTO"
	COMMAND_RPTS    Command = "RPTS"
	COMMAND_RPTSBKN Command = "RPTSBKN"
)

type FrameType uint

func (r *FrameType) ExtensionType() int8 { return 95 }

func (r *FrameType) Len() int { return 1 }

func (r *FrameType) MarshalBinaryTo(b []byte) error {
	b[0] = byte(*r)
	return nil
}

func (r *FrameType) UnmarshalBinary(b []byte) error {
	*r = FrameType(b[0])
	return nil
}

func (r *FrameType) String() string {
	switch *r {
	case FRAME_VOICE:
		return "Voice"
	case FRAME_VOICE_SYNC:
		return "Voice Sync"
	case FRAME_DATA_SYNC:
		return "Data Sync"
	default:
		return "Unknown"
	}
}

const (
	FRAME_VOICE      FrameType = 0x0
	FRAME_VOICE_SYNC FrameType = 0x1
	FRAME_DATA_SYNC  FrameType = 0x2
)

type DataType uint

const (
	DTYPE_VOICE_HEAD DataType = 0x1
	DTYPE_VOICE_TERM DataType = 0x2
)

var CallsignRegex = regexp.MustCompile(`^([A-Z0-9]{0,8})$`)
