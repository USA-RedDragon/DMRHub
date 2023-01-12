package main

const (
	// Command Types
	COMMAND_DMRA    = "DMRA"
	COMMAND_DMRD    = "DMRD"
	COMMAND_MSTCL   = "MSTCL"
	COMMAND_MSTNAK  = "MSTNAK"
	COMMAND_MSTPONG = "MSTPONG"
	COMMAND_MSTN    = "MSTN"
	COMMAND_MSTP    = "MSTP"
	COMMAND_MSTC    = "MSTC"
	COMMAND_RPTL    = "RPTL" // RPTLogin -- a repeater wants to login
	COMMAND_RPTPING = "RPTPING"
	COMMAND_RPTCL   = "RPTCL" // repeater wants to disconnect
	COMMAND_RPTACK  = "RPTACK"
	COMMAND_RPTK    = "RPTK" // Login challenge response
	COMMAND_RPTC    = "RPTC" // repeater wants to send config or disconnect
	COMMAND_RPTP    = "RPTP"
	COMMAND_RPTA    = "RPTA"
	COMMAND_RPTO    = "RPTO"
	COMMAND_RPTS    = "RPTS"
	COMMAND_RPTSBKN = "RPTSBKN"

	HBPF_VOICE      = 0x0
	HBPF_VOICE_SYNC = 0x1
	HBPF_DATA_SYNC  = 0x2

	HBPF_SLT_VHEAD = 0x1
	HBPF_SLT_VTERM = 0x2
)
