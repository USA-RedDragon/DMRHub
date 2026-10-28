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
	"errors"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"
)

// RoutingRuleKind is what a RoutingRule does.
type RoutingRuleKind string

const (
	// RoutingRuleSlotUser opens a slot of a repeater to the user SubjectID.
	// A slot with one or more of these rules carries only the traffic of
	// the radios of its users, in and out.
	RoutingRuleSlotUser RoutingRuleKind = "slot_user"
	// RoutingRuleBlockTalkgroup keeps the talkgroup SubjectID off a
	// repeater.
	RoutingRuleBlockTalkgroup RoutingRuleKind = "block_talkgroup"
	// RoutingRuleAllowTalkgroup lets the talkgroup SubjectID on a
	// repeater. A repeater with one or more of these rules carries only
	// those talkgroups.
	RoutingRuleAllowTalkgroup RoutingRuleKind = "allow_talkgroup"
	// RoutingRuleRestrictedTalkgroup makes the talkgroup SubjectID link
	// only to the repeaters that RoutingRuleApprovedLink rules approve. Its
	// RepeaterID is 0.
	RoutingRuleRestrictedTalkgroup RoutingRuleKind = "restricted_talkgroup"
	// RoutingRuleApprovedLink lets the repeater link the restricted
	// talkgroup SubjectID.
	RoutingRuleApprovedLink RoutingRuleKind = "approved_link"
)

// SlotRuleKinds returns the kinds of the rules of the slots of a repeater.
func SlotRuleKinds() []RoutingRuleKind {
	return []RoutingRuleKind{RoutingRuleSlotUser}
}

// TalkgroupRuleKinds returns the kinds of the rules of the talkgroup list
// of a repeater.
func TalkgroupRuleKinds() []RoutingRuleKind {
	return []RoutingRuleKind{RoutingRuleBlockTalkgroup, RoutingRuleAllowTalkgroup}
}

// ErrRoutingRuleKind is the error of a rule of a kind that the call does
// not replace.
var ErrRoutingRuleKind = errors.New("the rule is of a kind that is not replaced")

// RoutingRule is one rule of the routing of a repeater.
type RoutingRule struct {
	ID         uint            `json:"id" gorm:"primarykey"`
	RepeaterID uint            `json:"repeater_id" gorm:"not null;uniqueIndex:idx_routing_rule"`
	Kind       RoutingRuleKind `json:"kind" gorm:"size:32;not null;uniqueIndex:idx_routing_rule"`
	// Slot is 1 or 2 for a rule of a slot, and 0 for a rule of the
	// repeater.
	Slot      uint8     `json:"slot" gorm:"not null;uniqueIndex:idx_routing_rule"`
	SubjectID uint      `json:"subject_id" gorm:"not null;uniqueIndex:idx_routing_rule"`
	CreatedAt time.Time `json:"created_at"`
}

// ListRoutingRules returns the routing rules of the repeater repeaterID.
func ListRoutingRules(db *gorm.DB, repeaterID uint) ([]RoutingRule, error) {
	var rules []RoutingRule
	if err := db.Where("repeater_id = ?", repeaterID).Order("kind, slot, subject_id").Find(&rules).Error; err != nil {
		return nil, fmt.Errorf("could not list the routing rules of repeater %d: %w", repeaterID, err)
	}
	return rules, nil
}

// ReplaceRoutingRules replaces the routing rules of the kinds of the
// repeater repeaterID with rules, in one transaction. Each rule must be of
// one of kinds.
func ReplaceRoutingRules(db *gorm.DB, repeaterID uint, kinds []RoutingRuleKind, rules []RoutingRule) error {
	type key struct {
		kind    RoutingRuleKind
		slot    uint8
		subject uint
	}
	seen := make(map[key]bool, len(rules))
	rows := make([]RoutingRule, 0, len(rules))
	for _, r := range rules {
		if !slices.Contains(kinds, r.Kind) {
			return fmt.Errorf("%w: %s", ErrRoutingRuleKind, r.Kind)
		}
		k := key{r.Kind, r.Slot, r.SubjectID}
		if seen[k] {
			continue
		}
		seen[k] = true
		rows = append(rows, RoutingRule{RepeaterID: repeaterID, Kind: r.Kind, Slot: r.Slot, SubjectID: r.SubjectID})
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("repeater_id = ? AND kind IN ?", repeaterID, kinds).Delete(&RoutingRule{}).Error; err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("create: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not replace the routing rules of repeater %d: %w", repeaterID, err)
	}
	return nil
}

// TalkgroupAccess returns whether the talkgroup tg links only to approved
// repeaters, and the repeaters that it approves.
func TalkgroupAccess(db *gorm.DB, tg uint) (bool, []uint, error) {
	var rules []RoutingRule
	err := db.Where("subject_id = ? AND kind IN ?", tg, []RoutingRuleKind{RoutingRuleRestrictedTalkgroup, RoutingRuleApprovedLink}).
		Order("repeater_id").Find(&rules).Error
	if err != nil {
		return false, nil, fmt.Errorf("could not read the access of talkgroup %d: %w", tg, err)
	}
	restricted := false
	repeaters := []uint{}
	for _, r := range rules {
		if r.Kind == RoutingRuleRestrictedTalkgroup {
			restricted = true
		} else {
			repeaters = append(repeaters, r.RepeaterID)
		}
	}
	return restricted, repeaters, nil
}

// SetTalkgroupAccess sets whether the talkgroup tg links only to approved
// repeaters, and the repeaters that it approves, in one transaction.
func SetTalkgroupAccess(db *gorm.DB, tg uint, restricted bool, repeaters []uint) error {
	var rows []RoutingRule
	if restricted {
		rows = append(rows, RoutingRule{Kind: RoutingRuleRestrictedTalkgroup, SubjectID: tg})
	}
	seen := map[uint]bool{}
	for _, id := range repeaters {
		if !seen[id] {
			seen[id] = true
			rows = append(rows, RoutingRule{RepeaterID: id, Kind: RoutingRuleApprovedLink, SubjectID: tg})
		}
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("subject_id = ? AND kind IN ?", tg, []RoutingRuleKind{RoutingRuleRestrictedTalkgroup, RoutingRuleApprovedLink}).Delete(&RoutingRule{}).Error; err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		if err := tx.Create(&rows).Error; err != nil {
			return fmt.Errorf("create: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not set the access of talkgroup %d: %w", tg, err)
	}
	return nil
}

// LinkRefused returns the first of tgs that links only to approved
// repeaters and does not approve the repeater repeaterID, or 0 when the
// repeater may link them all.
func LinkRefused(db *gorm.DB, repeaterID uint, tgs ...uint) (uint, error) {
	if len(tgs) == 0 {
		return 0, nil
	}
	var restricted []uint
	err := db.Model(&RoutingRule{}).Where("kind = ? AND subject_id IN ?", RoutingRuleRestrictedTalkgroup, tgs).Pluck("subject_id", &restricted).Error
	if err != nil {
		return 0, fmt.Errorf("could not read restricted talkgroups: %w", err)
	}
	if len(restricted) == 0 {
		return 0, nil
	}
	var approved []uint
	err = db.Model(&RoutingRule{}).Where("kind = ? AND repeater_id = ? AND subject_id IN ?", RoutingRuleApprovedLink, repeaterID, restricted).Pluck("subject_id", &approved).Error
	if err != nil {
		return 0, fmt.Errorf("could not read approved links: %w", err)
	}
	for _, tg := range tgs {
		if slices.Contains(restricted, tg) && !slices.Contains(approved, tg) {
			return tg, nil
		}
	}
	return 0, nil
}
