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

package models

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// PeerRuleMatch names the ID of a call that a peer rule checks.
type PeerRuleMatch string

const (
	// PeerRuleMatchSource checks the radio ID of the caller.
	PeerRuleMatchSource PeerRuleMatch = "source"
	// PeerRuleMatchDestination checks the destination ID of the call: the
	// talkgroup of a group call, the target of a private call.
	PeerRuleMatchDestination PeerRuleMatch = "destination"
)

// Valid reports whether m is a match that a rule can have.
func (m PeerRuleMatch) Valid() bool {
	return m == PeerRuleMatchSource || m == PeerRuleMatchDestination
}

// DefaultPeerRuleMatch is the match of a rule that names none: the
// destination for an ingress rule, the source for an egress rule.
func DefaultPeerRuleMatch(ingress bool) PeerRuleMatch {
	if ingress {
		return PeerRuleMatchDestination
	}
	return PeerRuleMatchSource
}

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
	// Match is the ID of a call that the rule checks.
	Match PeerRuleMatch `json:"match" gorm:"column:match_kind;size:16;not null;default:''"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

// BeforeCreate gives a rule with no match the default of its direction.
func (p *PeerRule) BeforeCreate(*gorm.DB) error {
	if p.Match == "" {
		p.Match = DefaultPeerRuleMatch(p.Direction)
	}
	return nil
}

func (p *PeerRule) String() string {
	jsn, err := json.Marshal(p)
	if err != nil {
		slog.Error("Failed to marshal peer rule to json", "error", err)
		return ""
	}
	return string(jsn)
}

func ListRulesForPeer(db *gorm.DB, peerID uint) ([]PeerRule, error) {
	var peerRules []PeerRule
	err := db.Preload("Peer").Order("id asc").Where("peer_id = ?", peerID).Find(&peerRules).Error
	return peerRules, err
}

func ListIngressRulesForPeer(db *gorm.DB, peerID uint) ([]PeerRule, error) {
	var peerRules []PeerRule
	err := db.Preload("Peer").Order("id asc").Where("peer_id = ? AND direction = true", peerID).Find(&peerRules).Error
	return peerRules, err
}

func ListEgressRulesForPeer(db *gorm.DB, peerID uint) ([]PeerRule, error) {
	var peerRules []PeerRule
	err := db.Preload("Peer").Order("id asc").Where("peer_id = ? AND direction = false", peerID).Find(&peerRules).Error
	return peerRules, err
}

func FindPeerRuleByID(db *gorm.DB, id uint) (PeerRule, error) {
	var rule PeerRule
	err := db.Preload("Peer").First(&rule, id).Error
	return rule, err
}

func DeletePeerRule(db *gorm.DB, id uint) error {
	if err := db.Unscoped().Delete(&PeerRule{}, id).Error; err != nil {
		return fmt.Errorf("delete peer rule: %w", err)
	}
	return nil
}
