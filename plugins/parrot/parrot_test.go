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

package parrot

import (
	"bufio"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// capturedCall returns the call of DMRGateway in
// ../../internal/dmr/testdata/dmrgateway-master.txt, 3191868 to talkgroup
// 91 in one superframe, as bursts sent to the parrot at 91 as a private
// call.
func capturedCall(t *testing.T) []*pluginv1.Burst {
	t.Helper()
	f, err := os.Open("../../internal/dmr/testdata/dmrgateway-master.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []*pluginv1.Burst
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) != 4 || fs[1] != "r>m" || !strings.HasPrefix(fs[3], "444d5244") {
			continue
		}
		p, err := hex.DecodeString(fs[3])
		if err != nil {
			t.Fatal(err)
		}
		d, err := dmrd.DecodeData(p)
		if err != nil {
			t.Fatalf("%x does not decode: %v", p, err)
		}
		d.Private = true
		out = append(out, plugin.BurstOf(&d))
	}
	if len(out) != 8 {
		t.Fatalf("capture has %d DMRD packets, want 8", len(out))
	}
	return out
}

// testParrot returns a parrot that does not wait and the bursts it sends.
func testParrot() (*Parrot, func() []*pluginv1.Burst) {
	var mu sync.Mutex
	var sent []*pluginv1.Burst
	p := NewParrot(func(b *pluginv1.Burst) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, b)
		return nil
	})
	p.sleep = func(time.Duration) {}
	return p, func() []*pluginv1.Burst {
		p.Wait()
		mu.Lock()
		defer mu.Unlock()
		return sent
	}
}

func TestParrotPlaysTheCallBack(t *testing.T) {
	t.Parallel()
	p, sent := testParrot()
	call := capturedCall(t)
	for _, b := range call {
		p.Handle(b)
	}
	reply := sent()
	if len(reply) != 8 {
		t.Fatalf("reply has %d bursts, want 8", len(reply))
	}
	stream := reply[0].GetStreamId()
	if stream == call[0].GetStreamId() {
		t.Fatal("reply has the stream of the call")
	}
	for i, b := range reply {
		if b.GetSource() != 91 || b.GetDestination() != 3191868 || b.GetGroupCall() || b.GetStreamId() != stream || b.GetSequence() != uint32(i) || b.GetSlot() != call[0].GetSlot() {
			t.Fatalf("burst %d: %v", i, b)
		}
	}
	for _, i := range []int{0, 7} {
		dt := enums.DataTypeVoiceLCHeader
		if i == 7 {
			dt = enums.DataTypeTerminatorWithLC
		}
		if reply[i].GetFrameType() != pluginv1.FrameType_FRAME_TYPE_DATA_SYNC || reply[i].GetDataTypeOrVoiceSequence() != uint32(dt) {
			t.Fatalf("burst %d: %v, want data type %s", i, reply[i], dt)
		}
		cw, ok := layer2.RawBurst(reply[i].GetPayload()).MMDVMHostPayload(dt)
		if !ok {
			t.Fatalf("burst %d: Full LC does not decode", i)
		}
		lc := pdu.LCBytes(cw[:9])
		group, target, source, ok := lc.Call()
		if !ok || group || target != 3191868 || source != 91 {
			t.Fatalf("burst %d: LC %x, want a private call from 91 to 3191868", i, cw)
		}
	}
	for i := 1; i < 7; i++ {
		want := pluginv1.FrameType_FRAME_TYPE_VOICE
		if i == 1 {
			want = pluginv1.FrameType_FRAME_TYPE_VOICE_SYNC
		}
		if reply[i].GetFrameType() != want || reply[i].GetDataTypeOrVoiceSequence() != uint32(i-1) {
			t.Fatalf("burst %d: %v", i, reply[i])
		}
		got, rec := layer2.RawBurst(reply[i].GetPayload()).VoiceSlots(), layer2.RawBurst(call[i].GetPayload()).VoiceSlots()
		if got != rec {
			t.Fatalf("burst %d: voice %x, want the recorded %x", i, got, rec)
		}
	}
}

func TestParrotWaitsBeforeTheReply(t *testing.T) {
	t.Parallel()
	p, sent := testParrot()
	var mu sync.Mutex
	var waits []time.Duration
	p.sleep = func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		waits = append(waits, d)
	}
	for _, b := range capturedCall(t) {
		p.Handle(b)
	}
	sent()
	if len(waits) != 9 || waits[0] != replyDelay || waits[1] != burstTime {
		t.Fatalf("waits %v, want %v and then %v after each burst", waits, replyDelay, burstTime)
	}
}

func TestParrotIgnoresARepeatedTerminator(t *testing.T) {
	t.Parallel()
	p, sent := testParrot()
	call := capturedCall(t)
	for _, b := range call {
		p.Handle(b)
	}
	end := call[len(call)-1]
	p.Handle(end)
	p.Handle(end)
	if got := len(sent()); got != 8 {
		t.Fatalf("sent %d bursts, want 8", got)
	}
}

func TestParrotForgetsAnEndedCall(t *testing.T) {
	t.Parallel()
	p, sent := testParrot()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	call := capturedCall(t)
	for _, b := range call {
		p.Handle(b)
	}
	now = now.Add(forget + time.Second)
	for _, b := range call {
		p.Handle(b)
	}
	if got := len(sent()); got != 16 {
		t.Fatalf("sent %d bursts, want two replies of 8", got)
	}
}

func TestParrotAnswersAGroupCallWithAPrivateCall(t *testing.T) {
	t.Parallel()
	p, sent := testParrot()
	for _, b := range capturedCall(t) {
		g, _ := proto.Clone(b).(*pluginv1.Burst)
		g.GroupCall = true
		p.Handle(g)
	}
	reply := sent()
	require.Len(t, reply, 8)
	for i, b := range reply {
		if b.GetGroupCall() || b.GetSource() != 91 || b.GetDestination() != 3191868 {
			t.Fatalf("burst %d: %v, want a private call from 91 to 3191868", i, b)
		}
	}
}

func TestParrotIgnoresData(t *testing.T) {
	t.Parallel()
	p, sent := testParrot()
	call := capturedCall(t)
	csbk := &pluginv1.Burst{Source: 3191868, Destination: 91, Slot: 1, StreamId: 7, FrameType: pluginv1.FrameType_FRAME_TYPE_DATA_SYNC, DataTypeOrVoiceSequence: uint32(enums.DataTypeCSBK), Payload: make([]byte, 33)}
	p.Handle(csbk)
	p.Handle(call[len(call)-1])
	if got := sent(); len(got) != 0 {
		t.Fatalf("sent %d bursts, want none", len(got))
	}
}

func TestParrotDropsACallThatDoesNotEnd(t *testing.T) {
	t.Parallel()
	p, sent := testParrot()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	call := capturedCall(t)
	for _, b := range call[:len(call)-1] {
		p.Handle(b)
	}
	now = now.Add(idle + time.Second)
	other, _ := proto.Clone(call[0]).(*pluginv1.Burst)
	other.StreamId++
	p.Handle(other)
	p.mu.Lock()
	_, kept := p.calls[call[0].GetStreamId()]
	p.mu.Unlock()
	if kept {
		t.Fatal("the parrot keeps a call that has not ended")
	}
	p.Handle(call[len(call)-1])
	if got := sent(); len(got) != 0 {
		t.Fatalf("sent %d bursts, want none", len(got))
	}
}

func TestParrotIgnoresLateBurstsOfAnEndedCall(t *testing.T) {
	t.Parallel()
	p, _ := testParrot()
	call := capturedCall(t)
	for _, b := range call {
		p.Handle(b)
	}
	p.Handle(call[3])
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.calls) != 0 {
		t.Fatalf("the parrot records %d calls after the end, want none", len(p.calls))
	}
}
