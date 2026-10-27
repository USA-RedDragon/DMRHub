// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package modem

import (
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

const (
	pocsagIdleFrequency = 433000000
	ysfTXHang           = 4
	p25TXHang           = 5
	nxdnTXHang          = 5
	oscOffset           = 128
	dcBias              = 128
	maxTXDelay          = 50
	maxUint32           = 1<<32 - 1
)

type appender interface {
	AppendBinary(dst []byte) ([]byte, error)
}

// rawPayload is a payload of text, such as the callsign of SEND_CWID.
type rawPayload string

func (p rawPayload) AppendBinary(dst []byte) ([]byte, error) {
	return append(dst, p...), nil
}

// frame returns the frame of command c with the payload of msg, or with
// no payload for a nil msg.
func frame(c mmdvm.Command, msg appender) ([]byte, error) {
	var payload []byte
	if msg != nil {
		var err error
		if payload, err = msg.AppendBinary(nil); err != nil {
			return nil, fmt.Errorf("encode %s: %w", c, err)
		}
	}
	f, err := mmdvm.AppendFrame(nil, c, payload)
	if err != nil {
		return nil, fmt.Errorf("frame %s: %w", c, err)
	}
	return f, nil
}

// level returns the octet of a level in percent, as MMDVMHost computes it.
func level(pct float64) uint8 {
	return uint8(min(max(float32(pct), 0), 100)*2.55 + 0.5)
}

func dcOffset(v int) uint8 {
	return uint8(min(max(v, -dcBias), dcBias-1) + dcBias) //nolint:gosec // clamped to -128..127 before the bias
}

func txDelay(ms int) uint8 {
	return uint8(min(max(ms/10, 0), maxTXDelay)) //nolint:gosec // clamped to 0..50
}

func colorCode(m *config.Modem) enums.ColorCode {
	return enums.ColorCode(min(m.ColorCode, 15)) //nolint:gosec // clamped to 0..15
}

func dmrDelay(m *config.Modem) uint8 {
	return uint8(min(max(m.DMRDelay, 0), 255)) //nolint:gosec // clamped to 0..255
}

// frequency returns f plus off in Hz, clamped to 32 bits, as MMDVMHost
// setRFParams adds the offsets.
func frequency(f uint, off int) uint32 {
	hz := int64(min(f, maxUint32)) + int64(off)
	return uint32(min(max(hz, 0), maxUint32)) //nolint:gosec // clamped to 32 bits
}

// setFreq is the SET_FREQ payload of m, as MMDVMHost setFrequency sends it
// with POCSAG off.
func setFreq(m *config.Modem) mmdvm.SetFreq {
	return mmdvm.SetFreq{
		RX:      frequency(m.RXFrequency, m.RXOffset),
		TX:      frequency(m.TXFrequency, m.TXOffset),
		RFLevel: level(m.RFLevel),
		POCSAG:  pocsagIdleFrequency,
	}
}

// configV2 is the SET_CONFIG payload of protocol 2 for m, as MMDVMHost
// setConfig2 sends it with DMR the only mode. Every TX level is
// m.TXLevel, as MMDVMHost sets the levels it is not given.
func configV2(m *config.Modem) mmdvm.ConfigV2 {
	tx := level(m.TXLevel)
	return mmdvm.ConfigV2{
		Simplex: !m.Duplex, Debug: m.Debug, PTTInvert: m.PTTInvert, TXInvert: m.TXInvert, RXInvert: m.RXInvert,
		DMR: true, TXDelay: txDelay(m.TXDelay), State: mmdvm.ModeIdle,
		TXDCOffset: dcOffset(m.TXDCOffset), RXDCOffset: dcOffset(m.RXDCOffset), RXLevel: level(m.RXLevel),
		CWIDTXLevel: tx, DStarTXLevel: tx, DMRTXLevel: tx, YSFTXLevel: tx, P25TXLevel: tx, NXDNTXLevel: tx,
		POCSAGTXLevel: tx, FMTXLevel: tx, YSFTXHang: ysfTXHang, P25TXHang: p25TXHang, NXDNTXHang: nxdnTXHang,
		DMRColorCode: colorCode(m), DMRDelay: dmrDelay(m), OscOffset: oscOffset,
	}
}

// configV1 is the SET_CONFIG payload of protocol 1 for m, as MMDVMHost
// setConfig1 sends it with DMR the only mode.
func configV1(m *config.Modem) mmdvm.ConfigV1 {
	tx := level(m.TXLevel)
	return mmdvm.ConfigV1{
		Simplex: !m.Duplex, Debug: m.Debug, PTTInvert: m.PTTInvert, TXInvert: m.TXInvert, RXInvert: m.RXInvert,
		DMR: true, TXDelay: txDelay(m.TXDelay), State: mmdvm.ModeIdle, RXLevel: level(m.RXLevel),
		CWIDTXLevel: tx, DMRColorCode: colorCode(m), DMRDelay: dmrDelay(m), OscOffset: oscOffset,
		DStarTXLevel: tx, DMRTXLevel: tx, YSFTXLevel: tx, P25TXLevel: tx,
		TXDCOffset: dcOffset(m.TXDCOffset), RXDCOffset: dcOffset(m.RXDCOffset), NXDNTXLevel: tx,
		YSFTXHang: ysfTXHang, POCSAGTXLevel: tx, FMTXLevel: tx, P25TXHang: p25TXHang, NXDNTXHang: nxdnTXHang,
	}
}

// configFrame is the SET_CONFIG frame of m for the protocol of the modem.
func configFrame(m *config.Modem, protocol uint8) ([]byte, error) {
	if protocol == 1 {
		c := configV1(m)
		return frame(mmdvm.CommandSetConfig, &c)
	}
	c := configV2(m)
	return frame(mmdvm.CommandSetConfig, &c)
}

func modeFrame(mode mmdvm.Mode) ([]byte, error) {
	return frame(mmdvm.CommandSetMode, &mmdvm.SetMode{Mode: mode})
}
