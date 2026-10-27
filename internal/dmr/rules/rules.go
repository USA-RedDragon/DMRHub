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

package rules

import (
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

func PeerShouldEgress(db *gorm.DB, peer models.Peer, packet *models.Packet) (bool, error) {
	if peer.Egress {
		egressRules, err := models.ListEgressRulesForPeer(db, peer.ID)
		if err != nil {
			return false, fmt.Errorf("failed to list egress rules for peer %d: %w", peer.ID, err)
		}
		return EgressAllows(egressRules, peer.EgressPrivate, packet), nil
	}
	return false, nil
}

// EgressAllows reports whether the egress rules of a peer let it get
// packet. It mirrors the SUB_ACL and TGID_ACL of an HBlink3 or FreeDMR
// OpenBridge system: for each match that has rules, one of them must cover
// the ID of packet that it checks, and a match with no rules does not
// restrict. With no rules at all, the peer gets nothing. With
// privateCalls, a private call skips the destination rules and needs only
// the source rules, so with no source rules every private call passes.
func EgressAllows(egressRules []models.PeerRule, privateCalls bool, packet *models.Packet) bool {
	return allows(egressRules, privateCalls, packet)
}

func PeerShouldIngress(db *gorm.DB, peer *models.Peer, packet *models.Packet) (bool, error) {
	if peer.Ingress {
		ingressRules, err := models.ListIngressRulesForPeer(db, peer.ID)
		if err != nil {
			return false, fmt.Errorf("failed to list ingress rules for peer %d: %w", peer.ID, err)
		}
		return IngressAllows(ingressRules, peer.IngressPrivate, packet), nil
	}
	return false, nil
}

// IngressAllows reports whether the ingress rules of a peer let packet
// in, by the same logic as EgressAllows.
func IngressAllows(ingressRules []models.PeerRule, privateCalls bool, packet *models.Packet) bool {
	return allows(ingressRules, privateCalls, packet)
}

func allows(rs []models.PeerRule, privateCalls bool, packet *models.Packet) bool {
	var hasSrc, srcOK, hasDst, dstOK bool
	for i := range rs {
		r := &rs[i]
		match := r.Match
		if match == "" {
			match = models.DefaultPeerRuleMatch(r.Direction)
		}
		switch match {
		case models.PeerRuleMatchSource:
			hasSrc = true
			srcOK = srcOK || (r.SubjectIDMin <= packet.Src && packet.Src <= r.SubjectIDMax)
		case models.PeerRuleMatchDestination:
			hasDst = true
			dstOK = dstOK || (r.SubjectIDMin <= packet.Dst && packet.Dst <= r.SubjectIDMax)
		}
	}
	if privateCalls && !packet.GroupCall {
		return !hasSrc || srcOK
	}
	return (hasSrc || hasDst) && (!hasSrc || srcOK) && (!hasDst || dstOK)
}
