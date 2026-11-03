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

// Command prompts encodes spoken prompts once into the frame files that
// DMRHub embeds in internal/prompts, and writes decoded copies for a
// listening check. It encodes and decodes AMBE+2 on the AMBE server at
// -ambe, and Codec2 with dmrgo.
//
//	go run ./hack/prompts -ambe 127.0.0.1:2460 -in src -out out [-ber 1,5,10] [-seed 1]
//
// -in holds one WAV file per clip: mono 16-bit PCM at 8 kHz. For each
// clip NAME, promptgen sets the level, pads the clip to whole voice bursts
// and writes:
//
//   - frames/NAME.ambe and frames/NAME.codec2: the 72-bit vocoder slots of
//     the clip, 9 bytes each, a multiple of 3 slots (whole bursts). The
//     AMBE slots carry the AMBE+2 FEC; the Codec2 slots are Codec2 2400
//     bit/s in the DMR framing of package codec2.
//   - wav/NAME.in.wav: the 8 kHz input after the level change.
//   - wav/NAME.ambe.wav and wav/NAME.codec2.wav: the slots decoded.
//   - wav/NAME.ambe-berN.wav: the AMBE slots sent as DMR voice bursts,
//     with each of the 216 voice bits of each burst flipped with a
//     probability of N percent, received from the bursts and decoded.
//
// It prints one line of measures per clip and version: peak and RMS level
// in dBFS, clipped samples, and for the bit error copies the frames that
// the decoder did not take as good.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
)

const (
	sampleRate    = 8000
	frameSamples  = 160
	slotsPerBurst = 3
	leadFrames    = 2
	tailFrames    = 2
	targetRMS     = -21.0
	maxPeak       = -6.0
	activeFloor   = -45.0
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "promptgen:", err)
		os.Exit(1)
	}
}

func run(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("promptgen", flag.ContinueOnError)
	in := fs.String("in", "", "directory of 8 kHz mono 16-bit WAV clips")
	out := fs.String("out", "", "output directory")
	berList := fs.String("ber", "1,5,10", "bit error rates in percent for the AMBE copies")
	seed := fs.Uint64("seed", 1, "seed of the bit errors")
	server := fs.String("ambe", "", "host:port of the AMBE server")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("bad flags: %w", err)
	}
	if *in == "" || *out == "" || *server == "" {
		return errors.New("-ambe, -in and -out are required")
	}
	ambeOf := func() (vocoder.Codec, error) {
		return transcode.DialAMBE(context.Background(), *server, time.Second)
	}
	var bers []float64
	for f := range strings.SplitSeq(*berList, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		p, err := strconv.ParseFloat(f, 64)
		if err != nil || p < 0 || p > 100 {
			return fmt.Errorf("bad bit error rate %q", f)
		}
		bers = append(bers, p)
	}
	names, err := filepath.Glob(filepath.Join(*in, "*.wav"))
	if err != nil {
		return fmt.Errorf("cannot list the clips: %w", err)
	}
	if len(names) == 0 {
		return fmt.Errorf("%s: no WAV files", *in)
	}
	slices.Sort(names)
	for _, d := range []string{"frames", "wav"} {
		if err := os.MkdirAll(filepath.Join(*out, d), 0o755); err != nil {
			return fmt.Errorf("cannot make %s: %w", d, err)
		}
	}
	_, _ = fmt.Fprintln(w, "clip\tversion\tseconds\tpeak_dbfs\trms_dbfs\tclipped\tbad_frames")
	for i, path := range names {
		name := strings.TrimSuffix(filepath.Base(path), ".wav")
		if err := clip(w, ambeOf, path, *out, name, bers, *seed+uint64(i)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func clip(w io.Writer, ambeOf func() (vocoder.Codec, error), path, out, name string, bers []float64, seed uint64) error {
	pcm, err := readWAV(path)
	if err != nil {
		return err
	}
	pcm = pad(level(pcm))
	if err := writeWAV(filepath.Join(out, "wav", name+".in.wav"), pcm); err != nil {
		return err
	}
	report(w, name, "in", pcm, -1)

	amb, err := ambeOf()
	if err != nil {
		return err
	}
	defer func() { _ = amb.Close() }()
	ambeSlots, err := encode(amb, pcm)
	if err != nil {
		return err
	}
	c2, err := codec2.New(nil)
	if err != nil {
		return fmt.Errorf("cannot make a Codec2 encoder: %w", err)
	}
	defer func() { _ = c2.Close() }()
	c2Slots, err := encode(c2, pcm)
	if err != nil {
		return err
	}
	if err := writeSlots(filepath.Join(out, "frames", name+".ambe"), ambeSlots); err != nil {
		return err
	}
	if err := writeSlots(filepath.Join(out, "frames", name+".codec2"), c2Slots); err != nil {
		return err
	}

	for _, v := range []struct {
		version string
		slots   [][9]byte
		codec   func() (vocoder.Codec, error)
	}{
		{"ambe", ambeSlots, ambeOf},
		{"codec2", c2Slots, func() (vocoder.Codec, error) { return codec2.New(nil) }},
	} {
		dec, err := v.codec()
		if err != nil {
			return err
		}
		got, bad, err := decode(dec, v.slots)
		_ = dec.Close()
		if err != nil {
			return err
		}
		if err := writeWAV(filepath.Join(out, "wav", name+"."+v.version+".wav"), got); err != nil {
			return err
		}
		report(w, name, v.version, got, bad)
	}

	bursts, err := toBursts(ambeSlots)
	if err != nil {
		return err
	}
	for _, p := range bers {
		rng := rand.New(rand.NewPCG(seed, math.Float64bits(p)))
		hit := make([]layer2.RawBurst, len(bursts))
		for i, b := range bursts {
			hit[i] = flip(b, p/100, rng)
		}
		var slots [][9]byte
		for _, b := range hit {
			s := b.VoiceSlots()
			slots = append(slots, s[:]...)
		}
		dec, err := ambeOf()
		if err != nil {
			return err
		}
		got, bad, err := decode(dec, slots)
		_ = dec.Close()
		if err != nil {
			return err
		}
		version := fmt.Sprintf("ambe-ber%02g", p)
		if err := writeWAV(filepath.Join(out, "wav", name+"."+version+".wav"), got); err != nil {
			return err
		}
		report(w, name, version, got, bad)
	}
	return nil
}

func dbfs(x float64) float64 {
	if x <= 0 {
		return -120
	}
	return 20 * math.Log10(x/32768)
}

// level scales pcm so that its frames above activeFloor have an RMS of
// targetRMS, or less when the peak would pass maxPeak.
func level(pcm []int16) []int16 {
	var sum float64
	var n int
	var peak float64
	for i := 0; i+frameSamples <= len(pcm); i += frameSamples {
		var e float64
		for _, s := range pcm[i : i+frameSamples] {
			e += float64(s) * float64(s)
			peak = max(peak, math.Abs(float64(s)))
		}
		if dbfs(math.Sqrt(e/frameSamples)) > activeFloor {
			sum += e
			n += frameSamples
		}
	}
	if n == 0 || peak == 0 {
		return pcm
	}
	gain := math.Pow(10, (targetRMS-dbfs(math.Sqrt(sum/float64(n))))/20)
	gain = min(gain, math.Pow(10, (maxPeak-dbfs(peak))/20))
	out := make([]int16, len(pcm))
	for i, s := range pcm {
		out[i] = int16(max(-32768, min(32767, math.Round(float64(s)*gain))))
	}
	return out
}

// pad adds leadFrames of silence before pcm and at least tailFrames after
// it, to a whole number of voice bursts.
func pad(pcm []int16) []int16 {
	n := leadFrames*frameSamples + len(pcm) + tailFrames*frameSamples
	burst := slotsPerBurst * frameSamples
	n = (n + burst - 1) / burst * burst
	out := make([]int16, n)
	copy(out[leadFrames*frameSamples:], pcm)
	return out
}

func encode(c vocoder.Codec, pcm []int16) ([][9]byte, error) {
	var slots [][9]byte
	for i := 0; i < len(pcm); i += frameSamples {
		s, err := c.Encode(pcm[i : i+frameSamples])
		if err != nil {
			return nil, fmt.Errorf("cannot encode a frame: %w", err)
		}
		slots = append(slots, s)
	}
	return slots, nil
}

func decode(c vocoder.Codec, slots [][9]byte) (pcm []int16, bad int, err error) {
	for _, s := range slots {
		var r vocoder.FrameReport
		pcm, r, err = c.Decode(pcm, s)
		if err != nil {
			return nil, 0, fmt.Errorf("cannot decode a frame: %w", err)
		}
		if r.Frame != vocoder.FrameGood {
			bad++
		}
	}
	return pcm, bad, nil
}

// toBursts puts the slots into the voice bursts of a call, as a repeater
// sends them, without the header and terminator.
func toBursts(slots [][9]byte) ([]layer2.RawBurst, error) {
	lc := pdu.FullLinkControl{FLCO: enums.FLCOUnitToUnitVoiceChannelUser, FID: enums.FIDStandardized}
	if err := lc.SetAddresses(3000001, 9999); err != nil {
		return nil, fmt.Errorf("bad addresses: %w", err)
	}
	tx, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{
		LC: lc, ColorCode: 1, Source: layer2.SyncSourceBS, Silence: vocoder.AMBESilenceSlot(),
	})
	if err != nil {
		return nil, fmt.Errorf("cannot make a voice transmitter: %w", err)
	}
	var bursts []layer2.RawBurst
	for _, s := range slots {
		b, err := tx.AddSlots(s)
		if err != nil {
			return nil, fmt.Errorf("cannot make a burst: %w", err)
		}
		for _, raw := range b {
			if kb, err := layer2.NewBurstFromBytes(raw); err == nil && kb.Kind == layer2.KindVoice {
				bursts = append(bursts, raw)
			}
		}
	}
	return bursts, nil
}

// flip flips each of the 216 voice bits of b with probability p. The 48
// sync or EMB bits in the middle of the burst stay as they are.
func flip(b layer2.RawBurst, p float64, rng *rand.Rand) layer2.RawBurst {
	for bit := range 8 * len(b) {
		if bit >= 108 && bit < 156 {
			continue
		}
		if rng.Float64() < p {
			b[bit/8] ^= 0x80 >> (bit % 8)
		}
	}
	return b
}

func report(w io.Writer, name, version string, pcm []int16, bad int) {
	var peak, e float64
	clipped := 0
	for _, s := range pcm {
		a := math.Abs(float64(s))
		peak = max(peak, a)
		e += a * a
		if s == math.MaxInt16 || s <= -math.MaxInt16 {
			clipped++
		}
	}
	rms := 0.0
	if len(pcm) > 0 {
		rms = math.Sqrt(e / float64(len(pcm)))
	}
	b := "-"
	if bad >= 0 {
		b = strconv.Itoa(bad)
	}
	_, _ = fmt.Fprintf(w, "%s\t%s\t%.2f\t%.1f\t%.1f\t%d\t%s\n", name, version,
		float64(len(pcm))/sampleRate, dbfs(peak), dbfs(rms), clipped, b)
}

func writeSlots(path string, slots [][9]byte) error {
	b := make([]byte, 0, 9*len(slots))
	for _, s := range slots {
		b = append(b, s[:]...)
	}
	return writeFile(path, b)
}

func readWAV(path string) ([]int16, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var fmtOK bool
	for p := 12; p+8 <= len(b); {
		id := string(b[p : p+4])
		size := int(binary.LittleEndian.Uint32(b[p+4:]))
		body := b[p+8:]
		if size > len(body) {
			size = len(body)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("short fmt chunk")
			}
			tag := binary.LittleEndian.Uint16(body)
			ch := binary.LittleEndian.Uint16(body[2:])
			rate := binary.LittleEndian.Uint32(body[4:])
			bits := binary.LittleEndian.Uint16(body[14:])
			if tag != 1 || ch != 1 || rate != sampleRate || bits != 16 {
				return nil, fmt.Errorf("want mono 16-bit PCM at 8 kHz, got tag %d, %d channels, %d Hz, %d bits", tag, ch, rate, bits)
			}
			fmtOK = true
		case "data":
			if !fmtOK {
				return nil, errors.New("data before fmt")
			}
			pcm := make([]int16, size/2)
			if _, err := binary.Decode(body[:size&^1], binary.LittleEndian, pcm); err != nil {
				return nil, fmt.Errorf("bad samples: %w", err)
			}
			return pcm, nil
		}
		p += 8 + size + size&1
	}
	return nil, errors.New("no data chunk")
}

func writeWAV(path string, pcm []int16) error {
	n := uint32(2 * len(pcm))
	b := make([]byte, 0, 44+n)
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, 36+n)
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint32(b, sampleRate)
	b = binary.LittleEndian.AppendUint32(b, 2*sampleRate)
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, n)
	b, err := binary.Append(b, binary.LittleEndian, pcm)
	if err != nil {
		return fmt.Errorf("cannot make %s: %w", path, err)
	}
	return writeFile(path, b)
}

func writeFile(path string, b []byte) error {
	if err := os.WriteFile(path, b, 0o644); err != nil { //nolint:gosec // the frames and WAV files are not secret
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}
