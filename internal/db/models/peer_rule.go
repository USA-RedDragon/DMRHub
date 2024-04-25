// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package models

import (
	"encoding/json"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"gorm.io/gorm"
)

// PeerRule is the model for an OpenBridge DMR peer's routing rules
type PeerRule struct {
	ID     uint `json:"id" gorm:"primarykey"`
	PeerID uint `json:"-"`
	Peer   Peer `json:"peer" gorm:"foreignKey:PeerID"`

	// Direction is true for ingress, false for egress
	Direction bool `json:"direction"`
	// SubjectID is the ID of the subject (talkgroup/user/repeater)
	SubjectIDMin uint `json:"subject_id_min"`
	SubjectIDMax uint `json:"subject_id_max"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (p *PeerRule) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		logging.Errorf("Failed to marshal peer rule to json: %s", err)
		return ""
	}
	return string(jsn)
}

func ListRulesForPeer(db *gorm.DB, peerID uint) []PeerRule {
	var peerRules []PeerRule
	db.Preload("Peer").Order("id asc").Where("peer_id = ?", peerID).Find(&peerRules)
	return peerRules
}

func ListIngressRulesForPeer(db *gorm.DB, peerID uint) []PeerRule {
	var peerRules []PeerRule
	db.Preload("Peer").Order("id asc").Where("peer_id = ? AND direction = true", peerID).Find(&peerRules)
	return peerRules
}

func ListEgressRulesForPeer(db *gorm.DB, peerID uint) []PeerRule {
	var peerRules []PeerRule
	db.Preload("Peer").Order("id asc").Where("peer_id = ? AND direction = false", peerID).Find(&peerRules)
	return peerRules
}
