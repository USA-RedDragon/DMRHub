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
package rules_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/stretchr/testify/assert"
)

func rule(ingress bool, match models.PeerRuleMatch, first, last uint) models.PeerRule {
	return models.PeerRule{Direction: ingress, Match: match, SubjectIDMin: first, SubjectIDMax: last}
}

func groupCall(src, tg uint) *models.Packet {
	return &models.Packet{Src: src, Dst: tg, GroupCall: true}
}

func privateCall(dst uint) *models.Packet {
	return &models.Packet{Src: 3000050, Dst: dst}
}

func TestEgressRuleMatches(t *testing.T) {
	t.Parallel()
	const (
		src   = models.PeerRuleMatchSource
		dst   = models.PeerRuleMatchDestination
		radio = 3000050
	)
	cases := []struct {
		name   string
		rules  []models.PeerRule
		packet *models.Packet
		want   bool
	}{
		{"no rules", nil, groupCall(radio, 3100), false},
		{"talkgroup rule sends its talkgroup", []models.PeerRule{rule(false, dst, 3100, 3100)}, groupCall(radio, 3100), true},
		{"talkgroup rule keeps other talkgroups", []models.PeerRule{rule(false, dst, 3100, 3100)}, groupCall(radio, 91), false},
		{"any talkgroup rule sends", []models.PeerRule{rule(false, dst, 3100, 3100), rule(false, dst, 31665, 31665)}, groupCall(radio, 31665), true},
		{"talkgroup rules ignore the caller", []models.PeerRule{rule(false, dst, 3100, 3100)}, groupCall(1, 3100), true},
		{"source rule sends its radios", []models.PeerRule{rule(false, src, 3000001, 3000099)}, groupCall(radio, 91), true},
		{"source rule keeps other radios", []models.PeerRule{rule(false, src, 3000001, 3000099)}, groupCall(3100001, 91), false},
		{"default match is the source", []models.PeerRule{rule(false, "", 3000001, 3000099)}, groupCall(radio, 91), true},
		{"default match ignores the talkgroup", []models.PeerRule{rule(false, "", 91, 91)}, groupCall(radio, 91), false},
		{"both kinds pass", []models.PeerRule{rule(false, dst, 3100, 3100), rule(false, src, 3000001, 3000099)}, groupCall(radio, 3100), true},
		{"both kinds, talkgroup fails", []models.PeerRule{rule(false, dst, 3100, 3100), rule(false, src, 3000001, 3000099)}, groupCall(radio, 91), false},
		{"both kinds, source fails", []models.PeerRule{rule(false, dst, 3100, 3100), rule(false, src, 3000001, 3000099)}, groupCall(3100001, 3100), false},
		{"talkgroup rule keeps a private call to another ID", []models.PeerRule{rule(false, dst, 3100, 3100)}, privateCall(3100001), false},
		{"destination rule sends a private call to its range", []models.PeerRule{rule(false, dst, 3100, 3100), rule(false, dst, 1000000, 9999999)}, privateCall(3100001), true},
		{"source rule sends a private call", []models.PeerRule{rule(false, src, 3000001, 3000099)}, privateCall(3100001), true},
		{"unknown match is ignored", []models.PeerRule{rule(false, "talkgroup", 1, 16777215)}, groupCall(radio, 3100), false},
		{"unknown match does not restrict", []models.PeerRule{rule(false, "talkgroup", 9, 9), rule(false, dst, 3100, 3100)}, groupCall(radio, 3100), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, rules.EgressAllows(c.rules, false, c.packet))
		})
	}
}

func TestIngressRuleMatches(t *testing.T) {
	t.Parallel()
	const (
		src   = models.PeerRuleMatchSource
		dst   = models.PeerRuleMatchDestination
		radio = 3000050
	)
	cases := []struct {
		name   string
		rules  []models.PeerRule
		packet *models.Packet
		want   bool
	}{
		{"no rules", nil, groupCall(radio, 3100), false},
		{"default match is the destination", []models.PeerRule{rule(true, "", 3100, 3100)}, groupCall(radio, 3100), true},
		{"default match ignores the caller", []models.PeerRule{rule(true, "", radio, radio)}, groupCall(radio, 3100), false},
		{"destination rule", []models.PeerRule{rule(true, dst, 3100, 3100)}, groupCall(radio, 91), false},
		{"source rule takes its radios", []models.PeerRule{rule(true, src, 3000001, 3000099)}, groupCall(radio, 91), true},
		{"source rule drops other radios", []models.PeerRule{rule(true, src, 3000001, 3000099)}, groupCall(3100001, 91), false},
		{"both kinds pass", []models.PeerRule{rule(true, dst, 3100, 3100), rule(true, src, 3000001, 3000099)}, groupCall(radio, 3100), true},
		{"both kinds, source fails", []models.PeerRule{rule(true, dst, 3100, 3100), rule(true, src, 3000001, 3000099)}, groupCall(3100001, 3100), false},
		{"destination rule takes a private call to its range", []models.PeerRule{rule(true, dst, 1000000, 9999999)}, privateCall(2000001), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, rules.IngressAllows(c.rules, false, c.packet))
		})
	}
}

func TestRuleMatchDoesNotAllocate(t *testing.T) { //nolint:paralleltest // AllocsPerRun counts the allocations of every test that runs
	rs := []models.PeerRule{
		rule(false, models.PeerRuleMatchDestination, 3100, 3100),
		rule(false, models.PeerRuleMatchSource, 3000001, 3000099),
	}
	p := groupCall(3000050, 3100)
	allocs := testing.AllocsPerRun(100, func() {
		_ = rules.EgressAllows(rs, true, p)
		_ = rules.IngressAllows(rs, true, p)
	})
	assert.Zero(t, allocs)
}

func TestPrivateCallSwitch(t *testing.T) {
	t.Parallel()
	const (
		src = models.PeerRuleMatchSource
		dst = models.PeerRuleMatchDestination
	)
	talkgroup := []models.PeerRule{rule(false, dst, 3100, 3100)}
	both := []models.PeerRule{rule(false, dst, 3100, 3100), rule(false, src, 3000001, 3000099)}
	cases := []struct {
		name    string
		rules   []models.PeerRule
		private bool
		packet  *models.Packet
		want    bool
	}{
		{"off keeps a private call that no destination rule covers", talkgroup, false, privateCall(3100001), false},
		{"on sends a private call past the destination rules", talkgroup, true, privateCall(3100001), true},
		{"on leaves group calls to the destination rules", talkgroup, true, groupCall(3000050, 91), false},
		{"on keeps the source rules", both, true, &models.Packet{Src: 3100001, Dst: 3100002}, false},
		{"on sends a private call from an allowed radio", both, true, privateCall(3100001), true},
		{"on with no rules sends private calls", nil, true, privateCall(3100001), true},
		{"on with no rules keeps group calls", nil, true, groupCall(3000050, 3100), false},
		{"off with no rules keeps private calls", nil, false, privateCall(3100001), false},
		{"on leaves group calls to the source rules", []models.PeerRule{rule(false, src, 3000001, 3000099)}, true, groupCall(3000050, 91), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, rules.EgressAllows(c.rules, c.private, c.packet))
			assert.Equal(t, c.want, rules.IngressAllows(c.rules, c.private, c.packet))
		})
	}
}
