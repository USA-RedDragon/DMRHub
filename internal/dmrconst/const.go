package dmrconst

import (
	"regexp"
)

// Command is a DMR command
type Command string

// Command Types
const (
	CommandDMRA    Command = "DMRA"    // DMR talker alias
	CommandDMRD    Command = "DMRD"    // DMR data
	CommandMSTCL   Command = "MSTCL"   // master server is closing connection
	CommandMSTNAK  Command = "MSTNAK"  // master -> repeater nak
	CommandMSTPONG Command = "MSTPONG" // RPTPING response
	CommandRPTL    Command = "RPTL"    // RPTLogin -- a repeater wants to login
	CommandRPTPING Command = "RPTPING" // repeater -> master ping
	CommandRPTCL   Command = "RPTCL"   // repeater wants to disconnect
	CommandRPTACK  Command = "RPTACK"  // mater -> repeater ack
	CommandRPTK    Command = "RPTK"    // Login challenge response
	CommandRPTC    Command = "RPTC"    // repeater wants to send config or disconnect
	CommandRPTO    Command = "RPTO"    // Repeater options. https://github.com/g4klx/MMDVMHost/blob/master/DMRplus_startup_options.md
	CommandRPTSBKN Command = "RPTSBKN" // Synchronous Site Beacon?
)

// FrameType is a DMR frame type
type FrameType uint

// ExtensionType returns the extension type for the frame
func (r *FrameType) ExtensionType() int8 { return 95 }

// Len returns the length of the frame
func (r *FrameType) Len() int { return 1 }

// MarshalBinaryTo writes the frame to the byte slice
func (r *FrameType) MarshalBinaryTo(b []byte) error {
	b[0] = byte(*r)
	return nil
}

// UnmarshalBinary reads the frame from the byte slice
func (r *FrameType) UnmarshalBinary(b []byte) error {
	*r = FrameType(b[0])
	return nil
}

// String returns the string representation of the frame
func (r *FrameType) String() string {
	switch *r {
	case FrameVoice:
		return "Voice"
	case FrameVoiceSync:
		return "Voice Sync"
	case FrameDataSync:
		return "Data Sync"
	default:
		return "Unknown"
	}
}

const (
	FrameVoice     FrameType = 0x0
	FrameVoiceSync FrameType = 0x1
	FrameDataSync  FrameType = 0x2
)

// DataType is a DMR data type
type DataType uint

const (
	DTypeVoiceHead DataType = 0x1
	DTypeVoiceTerm DataType = 0x2
)

// CallsignRegex is a regex for validating callsigns
var CallsignRegex = regexp.MustCompile(`^([A-Z0-9]{0,8})$`)
