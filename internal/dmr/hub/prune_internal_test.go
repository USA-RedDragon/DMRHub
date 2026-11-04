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
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAStalledRoutingWorkerKeepsTheStateOfItsStreams holds the routing
// worker of a source with a burst of a stream in it for longer than
// sightedStreamTTL, as a slow database can, while the hub prunes what
// idled. Each record of the stream must stay until the worker goes on, so
// that the burst finds them, and go at the first prune after that.
func TestAStalledRoutingWorkerKeepsTheStateOfItsStreams(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := NewHub(nil, nil, nil, nil)
		t.Cleanup(func() { h.Stop(context.Background()) })
		const stream, src = uint(0x5A11), uint(1000001)
		now := h.clock.Now()
		slot := repeaterSlot{repeater: 100001}
		target := streamTarget{stream: stream, dst: 91}
		claim := claimKey{stream: stream, src: src}
		h.sightedStreams.Store(sightingKey{src: src, stream: stream}, now)
		h.undeliverableLogged.Store(stream, now)
		h.loopLogged.Store(stream, now)
		h.privateDropLogged.Store(stream, now)
		h.unlockStreams.Store(unlockStream{stream: stream}, now)
		h.lockedStreams.Store(unlockStream{stream: stream}, now)
		h.streamTalkgroups.Store(target, now)
		h.streamPeers.Store(target, streamPeerSet{at: now})
		h.streamTargets.Store(target, targetAt{at: now})
		h.streamClaims.Store(claim, streamClaim{checked: now})
		h.streamPolicies.Store(claim, streamPolicy{start: now, last: now})
		h.slotRefused.Store(slotStream{repeaterSlot: slot, stream: stream, src: src}, now)
		h.slotHolders.Store(slot, slotHolder{stream: stream, src: src, last: now})
		h.drops.Store(slot, dropRecord{stream: stream, src: src, last: now})
		h.copyReports.Store(copyReport{stream: stream}, now)
		h.codecStreams.Store(stream, &streamCodec{last: now})

		kept := func() map[string]bool {
			_, sighted := h.sightedStreams.Load(sightingKey{src: src, stream: stream})
			_, undeliverable := h.undeliverableLogged.Load(stream)
			_, loop := h.loopLogged.Load(stream)
			_, privateDrop := h.privateDropLogged.Load(stream)
			_, unlock := h.unlockStreams.Load(unlockStream{stream: stream})
			_, locked := h.lockedStreams.Load(unlockStream{stream: stream})
			_, talkgroups := h.streamTalkgroups.Load(target)
			_, peers := h.streamPeers.Load(target)
			_, targets := h.streamTargets.Load(target)
			_, claims := h.streamClaims.Load(claim)
			_, policies := h.streamPolicies.Load(claim)
			_, refused := h.slotRefused.Load(slotStream{repeaterSlot: slot, stream: stream, src: src})
			_, holders := h.slotHolders.Load(slot)
			_, drops := h.drops.Load(slot)
			_, copies := h.copyReports.Load(copyReport{stream: stream})
			_, codecs := h.codecStreams.Load(stream)
			return map[string]bool{
				"sightedStreams": sighted, "undeliverableLogged": undeliverable, "loopLogged": loop,
				"privateDropLogged": privateDrop, "unlockStreams": unlock, "lockedStreams": locked,
				"streamTalkgroups": talkgroups, "streamPeers": peers, "streamTargets": targets,
				"streamClaims": claims, "streamPolicies": policies, "slotRefused": refused,
				"slotHolders": holders, "drops": drops, "copyReports": copies, "codecStreams": codecs,
			}
		}
		all := func(v bool) map[string]bool {
			out := kept()
			for k := range out {
				out[k] = v
			}
			return out
		}

		release := make(chan struct{})
		h.queueRouting(stream, 1)
		h.routing.Do(context.Background(), sourceSlot{slot: true}, func() {
			defer h.queueRouting(stream, -1)
			<-release
		})
		time.Sleep(2 * sightedStreamTTL)
		synctest.Wait()
		assert.Equal(t, all(true), kept(), "a prune dropped the state of a stream whose burst waited for routing")

		close(release)
		synctest.Wait()
		time.Sleep(2 * sightedStreamTTL)
		synctest.Wait()
		assert.Equal(t, all(false), kept(), "a prune kept the state of a stream after its routing went on")
	})
}
