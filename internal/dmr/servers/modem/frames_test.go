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
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

type captured struct {
	toModem bool
	frame   []byte
}

func readCapture(t testing.TB, name string) []captured {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []captured
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		p, err := hex.DecodeString(fs[1])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, captured{toModem: fs[0] == "h>m", frame: p})
	}
	return out
}

// mmdvmhostModem is the modem of testdata/mmdvmhost.txt: MMDVMHost
// 9384d68 with its MMDVM-Host.ini levels, simplex, color code 7 and the
// placeholder frequencies.
func mmdvmhostModem() config.Modem {
	return config.Modem{Enabled: true, Port: "/dev/null", Baud: 115200, RepeaterID: 311860, ColorCode: 7,
		RXFrequency: 430000000, TXFrequency: 440000000, RFLevel: 100, RXLevel: 50, TXLevel: 50,
		TXInvert: true, TXDelay: 100}
}

// hostFrame returns the first frame of command c that the host wrote.
func hostFrame(t testing.TB, frames []captured, c mmdvm.Command) []byte {
	t.Helper()
	for _, f := range frames {
		if f.toModem && len(f.frame) > 2 && mmdvm.Command(f.frame[2]) == c {
			return f.frame
		}
	}
	t.Fatalf("no %s frame in the capture", c)
	return nil
}

func mustFrame(t *testing.T, c mmdvm.Command, msg appender) []byte {
	t.Helper()
	f, err := frame(c, msg)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestOpenFramesAreThoseOfMMDVMHost(t *testing.T) {
	t.Parallel()
	frames := readCapture(t, "mmdvmhost.txt")
	m := mmdvmhostModem()
	freq := setFreq(&m)
	cfg, err := configFrame(&m, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{
		mustFrame(t, mmdvm.CommandGetVersion, nil),
		mustFrame(t, mmdvm.CommandSetFreq, &freq),
		cfg,
		mustFrame(t, mmdvm.CommandSetMode, &mmdvm.SetMode{Mode: mmdvm.ModeIdle}),
		mustFrame(t, mmdvm.CommandGetStatus, nil),
		mustFrame(t, mmdvm.CommandSetMode, &mmdvm.SetMode{Mode: mmdvm.ModeDMR}),
	}
	for i, w := range want {
		if !frames[i].toModem || !bytes.Equal(frames[i].frame, w) {
			t.Errorf("frame %d: MMDVMHost wrote %x, DMRHub writes %x", i, frames[i].frame, w)
		}
	}
}

// TestOpenFramesMatchPiStar checks the protocol 1 layout against the
// Pi-Star MMDVMHost of testdata/pistar.txt: duplex, RX level 40, RF level
// 35, TX delay 10 ms. That MMDVMHost has its own DMR TX level (0x83) and
// the M17 octets (0x80, 0x05) that MMDVMHost 9384d68 sends as 0; DMRHub
// sends one TX level for every mode, as MMDVMHost does without the
// per-mode levels.
func TestOpenFramesMatchPiStar(t *testing.T) {
	t.Parallel()
	frames := readCapture(t, "pistar.txt")
	m := config.Modem{Duplex: true, ColorCode: 1, RXFrequency: 430000000, TXFrequency: 440000000, RFLevel: 35,
		RXLevel: 40, TXLevel: 50, TXInvert: true, TXDelay: 10}
	freq := setFreq(&m)
	if got, want := mustFrame(t, mmdvm.CommandSetFreq, &freq), hostFrame(t, frames, mmdvm.CommandSetFreq); !bytes.Equal(got, want) {
		t.Errorf("SET_FREQ: Pi-Star %x, DMRHub %x", want, got)
	}
	got, err := configFrame(&m, 1)
	if err != nil {
		t.Fatal(err)
	}
	pistar := hostFrame(t, frames, mmdvm.CommandSetConfig)
	want := bytes.Clone(pistar)
	want[3+10] = 0x80
	want[3+21], want[3+22] = 0, 0
	if !bytes.Equal(got, want) {
		t.Errorf("SET_CONFIG: Pi-Star %x (with the 9384d68 octets %x), DMRHub %x", pistar, want, got)
	}
}

func TestDMRDataFramesAreThoseOfMMDVMHost(t *testing.T) {
	t.Parallel()
	n := 0
	for _, c := range readCapture(t, "mmdvmhost.txt") {
		f, _, res := mmdvm.Next(c.frame)
		if res != mmdvm.NextFrame || f.Command != mmdvm.CommandDMRData2 {
			continue
		}
		var b layer2.RawBurst
		copy(b[:], f.Payload[1:])
		d := mmdvm.DMRData{Slot: enums.Slot2, Burst: b}
		if got := mustFrame(t, d.Command(), &d); !bytes.Equal(got, c.frame) {
			t.Errorf("MMDVMHost wrote %x, DMRHub writes %x", c.frame, got)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no DMR data in the capture")
	}
}

// TestShortLCFollowsETSIWhereMMDVMHostSwapsTheSlots shows that the Short
// LC of the capture, for a group call on slot 2, has the activity in the
// TS1 field and the hash in the TS2 field: MMDVMHost DMRSlot::setShortLC
// puts the activity of slot 1 in the low nibble and of slot 2 in the high
// one, the reverse of ETSI TS 102 361-2 clause 7.1.3.2 (TS1_Act first), as
// SDRTrunk and DSD-FME decode it. DMRHub sends the ETSI layout.
func TestShortLCFollowsETSIWhereMMDVMHostSwapsTheSlots(t *testing.T) {
	t.Parallel()
	var got *mmdvm.DMRShortLC
	var target uint32
	for _, c := range readCapture(t, "mmdvmhost.txt") {
		f, _, res := mmdvm.Next(c.frame)
		if res != mmdvm.NextFrame {
			continue
		}
		switch f.Command { //nolint:exhaustive // the Short LC and the header of the call
		case mmdvm.CommandDMRShortLC:
			s, err := mmdvm.DecodeDMRShortLC(f.Payload)
			if err != nil {
				t.Fatal(err)
			}
			got = &s
		case mmdvm.CommandDMRData2:
			var b layer2.RawBurst
			copy(b[:], f.Payload[1:])
			if b.SlotType().DataType != enums.DataTypeVoiceLCHeader || target != 0 {
				continue
			}
			p, _ := b.BPTCPayload()
			lc, _, err := pdu.DecodeFullLCCodeword(p, enums.DataTypeVoiceLCHeader)
			if err != nil {
				t.Fatal(err)
			}
			lcb := pdu.LCBytes(lc)
			_, target, _, _ = lcb.Call()
		}
	}
	if got == nil || target == 0 {
		t.Fatal("no Short LC or Voice LC Header in the capture")
	}
	hash, err := pdu.AddressHash(target)
	if err != nil {
		t.Fatal(err)
	}
	slc := got.ShortLC()
	au, ok := slc.AsActivityUpdate()
	if !ok || au.TS1ActivityID != enums.ActivityGroupVoice || au.TS2ActivityID != enums.ActivityNoActivity ||
		au.HashTS1 != 0 || au.HashTS2 != hash {
		t.Fatalf("MMDVMHost Short LC %+v, want the slot 2 activity in TS1 and its hash %d in TS2", au, hash)
	}

	au = pdu.ShortLCActivityUpdate{TS2ActivityID: enums.ActivityGroupVoice, HashTS2: hash}
	var want pdu.ShortLC
	if err := want.SetActivityUpdate(au); err != nil {
		t.Fatal(err)
	}
	enc, err := mmdvm.NewDMRShortLC(&want)
	if err != nil {
		t.Fatal(err)
	}
	if enc == *got {
		t.Fatal("the ETSI Short LC is the MMDVMHost one")
	}
}
