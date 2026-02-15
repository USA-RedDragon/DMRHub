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
		for _, rule := range egressRules {
			if rule.SubjectIDMin <= packet.Src && rule.SubjectIDMax >= packet.Src {
				return true, nil
			}
		}
	}
	return false, nil
}

func PeerShouldIngress(db *gorm.DB, peer *models.Peer, packet *models.Packet) (bool, error) {
	if peer.Ingress {
		ingressRules, err := models.ListIngressRulesForPeer(db, peer.ID)
		if err != nil {
			return false, fmt.Errorf("failed to list ingress rules for peer %d: %w", peer.ID, err)
		}
		for _, rule := range ingressRules {
			if rule.SubjectIDMin <= packet.Dst && rule.SubjectIDMax >= packet.Dst {
				return true, nil
			}
		}
	}
	return false, nil
}
