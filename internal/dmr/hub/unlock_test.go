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

package hub_test

import (
	"context"
	"encoding/base32"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type unlockReply struct {
	locked chan hub.Unlock
	tried  chan airauth.Result
}

func newUnlockReply() *unlockReply {
	return &unlockReply{locked: make(chan hub.Unlock, 64), tried: make(chan airauth.Result, 64)}
}

func (r *unlockReply) Locked(u hub.Unlock) { r.locked <- u }

func (r *unlockReply) Tried(_ hub.Unlock, res airauth.Result) { r.tried <- res }

func (r *unlockReply) result(t *testing.T) airauth.Result {
	t.Helper()
	got := testutils.Collect(r.tried, failAfter, func(got []airauth.Result) bool { return len(got) > 0 })
	require.Len(t, got, 1, "the try of the code did not end")
	return got[0]
}

func unlockConfig() config.Config {
	c := floodConfig()
	c.Secret, c.PasswordSalt = "a-secret-of-fifteen", "a-salt-of-fifteen!"
	c.DMR.Routing.OnAirUnlock = time.Hour
	return c
}

// enrollOnAir turns the on-air code of listedUser on, with the time of the
// on-air codes held still at the time it returns.
func enrollOnAir(t *testing.T, h *hub.Hub) ([]byte, time.Time) {
	const user = listedUser
	t.Helper()
	now := time.Now()
	h.SetUnlockClock(clock.NewFake(now))
	ctx := context.Background()
	setup, err := h.OnAirCodes().Begin(ctx, user, "DMRHub", "n0list")
	require.NoError(t, err)
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Key)
	require.NoError(t, err)
	require.NoError(t, h.OnAirCodes().Confirm(ctx, user, airauth.Code(secret, airauth.Step(now)-1)))
	h.UserChanged(user)
	return secret, now
}

func (e *rulesEnv) sendCode(t *testing.T, src uint, code uint32) {
	t.Helper()
	stream := e.next
	e.next++
	for _, p := range []models.Packet{makeVoicePacket(dmrconst.UnlockFirst+uint(code), stream, false, false), makeVoiceTermPacket(dmrconst.UnlockFirst+uint(code), stream, false, false)} {
		p.Src, p.Repeater = src, ruleSender
		e.hub.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
}

func TestALockedRadioIDIsCarriedOnlyOnTheSlotThatItsCodeUnlocked(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.hub.SetConfig(unlockConfig())
		e.markerSrc = otherUser
		reply := newUnlockReply()
		e.hub.SetUnlockReplies(reply)
		secret, now := enrollOnAir(t, e.hub)

		assert.False(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}), "a radio ID of a user with the on-air code on starts locked")
		assert.False(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: 1}), "the account ID too")
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "a user without the on-air code is not locked")
		select {
		case u := <-reply.locked:
			assert.Equal(t, hub.Unlock{Server: models.RepeaterTypeMMDVM, Repeater: ruleSender, Src: listedRadio}, u)
		default:
			assert.Fail(t, "a locked transmission must be told to the replies")
		}

		e.sendCode(t, listedRadio, airauth.Code(secret, airauth.Step(now)+1)+1)
		assert.Equal(t, airauth.Refused, reply.result(t))
		assert.False(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}), "a wrong code unlocks nothing")

		e.sendCode(t, listedRadio, airauth.Code(secret, airauth.Step(now)+1))
		assert.Equal(t, airauth.Granted, reply.result(t))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: listedUser, private: true}), "private traffic of the unlocked radio too")
		assert.False(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: 3100, slot: true}), "the other slot stays locked")
		assert.False(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: 1}), "another radio ID of the user stays locked")

		radios, err := e.hub.OnAirCodes().Reset(context.Background(), listedUser)
		require.NoError(t, err)
		e.hub.UserChanged(listedUser)
		for _, r := range radios {
			e.hub.UnlocksChanged(r)
		}
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: 1}), "turning the code off unlocks every radio ID")
	})
}

func TestACallToACodeReachesNoOtherRepeater(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.hub.SetConfig(unlockConfig())
		e.markerSrc = otherUser
		reply := newUnlockReply()
		e.hub.SetUnlockReplies(reply)
		secret, now := enrollOnAir(t, e.hub)
		code := airauth.Code(secret, airauth.Step(now)+1)
		e.sendCode(t, listedRadio, code)
		require.Equal(t, airauth.Granted, reply.result(t))
		e.sendCode(t, otherUser, code)
		require.Equal(t, airauth.Refused, reply.result(t))
		e.hub.RoutePacket(context.Background(), func() models.Packet {
			p := makeVoicePacket(dmrconst.UnlockFirst+uint(code), 999, false, false)
			p.Src, p.Repeater = listedRadio, ruleSender
			return p
		}(), plugins.SourceName(7))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}))
		for {
			select {
			case rp := <-e.srv.Packets:
				assert.False(t, rp.Packet.IsUnlockCall(), "a call to a code must not reach a repeater")
			default:
				return
			}
		}
	})
}

func TestAnUnlockReachesEveryReplica(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.hub.SetConfig(unlockConfig())
		e.markerSrc = otherUser
		reply := newUnlockReply()
		e.hub.SetUnlockReplies(reply)
		secret, now := enrollOnAir(t, e.hub)

		h2 := newRulesHub(t, e.db, e.ps, e.kv)
		h2.SetConfig(unlockConfig())
		e2 := &rulesEnv{hub: h2, db: e.db, ps: e.ps, kv: e.kv, next: 1000, markerSrc: otherUser}
		e2.srv = h2.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		for _, rpt := range []uint{ruleSender, ruleReceiver} {
			h2.ActivateRepeater(context.Background(), rpt)
		}
		knowTalkgroups(t, h2)
		assert.False(t, e2.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}))

		changed := make(chan uint, 4)
		t.Cleanup(h2.OnUnlocksChange(func(radio uint) { changed <- radio }))
		e.sendCode(t, listedRadio, airauth.Code(secret, airauth.Step(now)+1))
		require.Equal(t, airauth.Granted, reply.result(t))
		require.Len(t, testutils.Collect(changed, failAfter, func(got []uint) bool { return got[len(got)-1] == listedRadio }), 1)
		assert.True(t, e2.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}), "the replica that took the code is not the only one that knows")
	})
}

func TestTurningTheCodeOnLocksTheRadioOnEveryReplica(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.hub.SetConfig(unlockConfig())
		e.markerSrc = otherUser
		h2 := newRulesHub(t, e.db, e.ps, e.kv)
		h2.SetConfig(unlockConfig())
		e2 := &rulesEnv{hub: h2, db: e.db, ps: e.ps, kv: e.kv, next: 1000, markerSrc: otherUser}
		e2.srv = h2.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		for _, rpt := range []uint{ruleSender, ruleReceiver} {
			h2.ActivateRepeater(context.Background(), rpt)
		}
		knowTalkgroups(t, h2)
		require.True(t, e2.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}), "the radio is free before the code is on")

		changed := make(chan uint, 4)
		t.Cleanup(h2.OnUserChange(func(user uint) { changed <- user }))
		enrollOnAir(t, e.hub)
		assert.False(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}), "the replica that turned the code on locks the radio at once")
		require.Len(t, testutils.Collect(changed, failAfter, func(got []uint) bool { return got[len(got)-1] == listedUser }), 1)
		assert.False(t, e2.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}), "another replica locks the radio once the notice comes")
	})
}
