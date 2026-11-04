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

package hub

import (
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
)

const (
	// promptQuiet is how long a slot must carry no refused burst before
	// the hub plays a prompt on it, so that the radio is back in receive.
	promptQuiet = time.Second
	// promptStreamTTL is how long the hub remembers a stream that got its
	// prompt, so that the late bursts of the stream get no second prompt.
	promptStreamTTL = time.Minute
	// promptSource is the server name of the source slots of prompts.
	promptSource = "prompts"
)

// Prompt is a spoken prompt for the radio Radio, played on a slot of a
// repeater or hotspot of the server Server.
type Prompt struct {
	Server   string
	Repeater uint
	Slot2    bool
	Radio    uint
	Clip     string
}

type promptSlot struct {
	server   string
	repeater uint
	slot     bool
}

type pendingPrompt struct {
	prompt Prompt
	cc     enums.ColorCode
	last   time.Time
}

type promptState struct {
	mu      sync.Mutex
	pending map[promptSlot]*pendingPrompt
	streams map[uint]time.Time
}

func (h *Hub) promptsOn() bool {
	return h.routingConfig().SpokenRefusals
}

// promptable reports whether the voice burst packet from sourceName comes
// from a repeater, hotspot or browser radio of this hub, which may get a
// prompt. Peers and plugins never do.
func (h *Hub) promptable(packet models.Packet, sourceName string) bool {
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return false
	}
	return packet.Src != 0 && packet.Repeater != 0
}

// promptRefused queues the prompt clip for the radio of packet, a burst
// that the hub refuses, once for its stream. The prompt plays promptQuiet
// after the last refused burst on the slot and after the last burst of the
// call that holds the slot, if one does.
func (h *Hub) promptRefused(packet models.Packet, sourceName, clip string) {
	if clip == "" || !h.promptsOn() || !h.promptable(packet, sourceName) {
		return
	}
	if isVoice, _ := utils.CheckPacketType(packet); !isVoice {
		return
	}
	h.queuePrompt(Prompt{Server: sourceName, Repeater: packet.Repeater, Slot2: packet.Slot, Radio: packet.Src, Clip: clip}, packet.StreamID, colorCodeOf(packet))
}

// Say plays the prompt p on its slot promptQuiet after the last refused
// or taken burst on that slot and after the last burst of the call that
// holds the slot, if one does, when spoken prompts are on.
func (h *Hub) Say(p Prompt) {
	if !h.promptsOn() {
		return
	}
	h.queuePrompt(p, 0, 1)
}

// heldBack moves the prompt that waits on the slot of packet, if any, to
// promptQuiet after now, as the slot still carries the radio.
func (h *Hub) heldBack(packet models.Packet, sourceName string) {
	key := promptSlot{server: sourceName, repeater: packet.Repeater, slot: packet.Slot}
	s := &h.promptState
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.pending[key]; p != nil {
		p.last = h.clock.Now()
	}
}

func (h *Hub) queuePrompt(p Prompt, stream uint, cc enums.ColorCode) {
	key := promptSlot{server: p.Server, repeater: p.Repeater, slot: p.Slot2}
	now := h.clock.Now()
	s := &h.promptState
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pending == nil {
		s.pending, s.streams = map[promptSlot]*pendingPrompt{}, map[uint]time.Time{}
	}
	if pending := s.pending[key]; pending != nil {
		pending.last = now
		return
	}
	if stream != 0 {
		for id, at := range s.streams {
			if now.Sub(at) > promptStreamTTL {
				delete(s.streams, id)
			}
		}
		if _, done := s.streams[stream]; done {
			return
		}
		s.streams[stream] = now
	}
	s.pending[key] = &pendingPrompt{prompt: p, cc: cc, last: now}
	h.clock.AfterFunc(promptQuiet, func() { h.firePrompt(key) })
}

func (h *Hub) firePrompt(key promptSlot) {
	s := &h.promptState
	s.mu.Lock()
	p := s.pending[key]
	if p == nil {
		s.mu.Unlock()
		return
	}
	last := p.last
	if held := h.heldUntil(key.repeater, key.slot); held.After(last) {
		last = held
	}
	if wait := promptQuiet - h.clock.Since(last); wait > 0 {
		s.mu.Unlock()
		h.clock.AfterFunc(wait, func() { h.firePrompt(key) })
		return
	}
	delete(s.pending, key)
	s.mu.Unlock()
	h.playPrompt(p.prompt, p.cc)
}

// heldUntil returns the time of the last burst of the call that holds the
// timeslot of the repeater, or the zero time when none does.
func (h *Hub) heldUntil(repeaterID uint, slot bool) time.Time {
	cur, _ := h.slotHolders.Load(repeaterSlot{repeater: repeaterID, slot: slot})
	return cur.last
}

// playPrompt sends p to its repeater slot as a private call from the
// service ID, in the voice of the owner of the radio or the default voice,
// one burst every 60 ms.
func (h *Hub) playPrompt(p Prompt, cc enums.ColorCode) {
	r := h.routingConfig()
	rpt, err := h.routingRepeater(p.Repeater)
	if err != nil {
		slog.Warn("Cannot play a prompt to a repeater that the hub cannot find", "repeaterID", p.Repeater, "error", err)
		return
	}
	src := r.PromptServiceID
	if src == 0 {
		src = prompts.DefaultServiceID
	}
	packets, err := prompts.Build(prompts.Call{
		Voice: h.promptVoice(p.Radio), Clip: p.Clip, Codec: transcode.ParseCodec(rpt.SlotCodec(p.Slot2)),
		Src: src, Dst: p.Radio, Repeater: p.Repeater, Slot2: p.Slot2, ColorCode: cc, Stream: newStreamID(),
	})
	if err != nil {
		slog.Error("Cannot build a prompt", "clip", p.Clip, "error", err)
		return
	}
	key := sourceSlot{streamSource: streamSource{server: promptSource, repeater: p.Repeater}, slot: p.Slot2}
	var send func(i int)
	send = func(i int) {
		h.publishToRepeater(key, p.Repeater, packets[i])
		if i+1 < len(packets) {
			h.clock.AfterFunc(prompts.Duration(1), func() { send(i + 1) })
		}
	}
	send(0)
}

// promptVoice returns the voice that the owner of radio picked, or the
// default voice of the hub.
func (h *Hub) promptVoice(radio uint) string {
	if h.db != nil {
		if owner, err := h.radioOwners.Get(radio); err == nil && owner != 0 {
			if u, err := models.FindUserByID(h.db, owner); err == nil && prompts.HasVoice(u.PromptVoice) {
				return u.PromptVoice
			}
		}
	}
	if v := h.routingConfig().PromptVoice; prompts.HasVoice(v) {
		return v
	}
	return prompts.DefaultVoice
}

func colorCodeOf(packet models.Packet) enums.ColorCode {
	if packet.FrameType == dmrconst.FrameDataSync {
		return layer2.RawBurst(packet.DMRData).SlotType().ColorCode
	}
	return 1
}

// promptReplies answers the on-air codes with spoken prompts.
type promptReplies struct{ h *Hub }

func (r promptReplies) Locked(u Unlock) {
	r.h.Say(Prompt{Server: u.Server, Repeater: u.Repeater, Slot2: u.Slot2, Radio: u.Src, Clip: prompts.CodeNeeded})
}

func (r promptReplies) Tried(u Unlock, res airauth.Result) {
	clip := ""
	switch res {
	case airauth.Granted:
		clip = prompts.Unlocked
	case airauth.Refused:
		clip = prompts.WrongCode
	case airauth.Limited, airauth.Unavailable:
		return
	}
	if clip != "" {
		r.h.Say(Prompt{Server: u.Server, Repeater: u.Repeater, Slot2: u.Slot2, Radio: u.Src, Clip: clip})
	}
}

// PromptReplies returns the UnlockReplies that speak "Code needed.",
// "Unlocked." and "Wrong code." when spoken prompts are on. The hub uses
// them until SetUnlockReplies sets others.
func (h *Hub) PromptReplies() UnlockReplies {
	return promptReplies{h: h}
}
