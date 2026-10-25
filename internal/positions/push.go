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

// Package positions pushes the new positions of tracked radios to the
// users who see them.
package positions

import (
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"gorm.io/gorm"
)

const (
	topicPrefix = "positions:"
	// AllTopic is the pubsub topic of every stored position, as JSON of
	// models.Position, for the APRS-IS gateway.
	AllTopic = "positions:all"
)

// Topic is the pubsub topic of the positions pushed to the user userID.
func Topic(userID uint) string {
	return topicPrefix + strconv.FormatUint(uint64(userID), 10)
}

// Pusher publishes each new position to the owner of its radio and, when
// the owner shares, to every other user.
type Pusher struct {
	db     *gorm.DB
	pubsub pubsub.PubSub
}

// NewPusher returns a Pusher; give its Push to the locator.
func NewPusher(db *gorm.DB, ps pubsub.PubSub) *Pusher {
	return &Pusher{db: db, pubsub: ps}
}

// Push publishes p on AllTopic and to the users who see it.
func (p *Pusher) Push(pos models.Position) {
	if data, err := json.Marshal(pos); err != nil {
		slog.Error("Position push: cannot encode a position", "error", err)
	} else if err := p.pubsub.Publish(AllTopic, data); err != nil {
		slog.Error("Position push: cannot publish a position", "error", err)
	}
	ownerID, ok, err := models.PositionOwner(p.db, pos.RadioID)
	if err != nil {
		slog.Error("Position push: cannot find the owner of a radio", "radio", pos.RadioID, "error", err)
		return
	}
	if !ok {
		return
	}
	var owner models.User
	if err := p.db.First(&owner, ownerID).Error; err != nil {
		slog.Error("Position push: cannot load a user", "user", ownerID, "error", err)
		return
	}
	if owner.Suspended || !owner.Approved {
		return
	}
	view := apimodels.NewPosition(&pos)
	p.publish(owner.ID, view)
	if !owner.SharePositions {
		return
	}
	var viewers []uint
	err = p.db.Model(&models.User{}).Where("approved = ? AND suspended = ? AND id <> ?", true, false, owner.ID).Pluck("id", &viewers).Error
	if err != nil {
		slog.Error("Position push: cannot list the users", "error", err)
		return
	}
	view.Shared, view.Callsign = true, owner.Callsign
	for _, id := range viewers {
		p.publish(id, view)
	}
}

func (p *Pusher) publish(userID uint, view apimodels.Position) {
	data, err := json.Marshal(apimodels.PositionPush{Kind: apimodels.PositionPushKind, Position: view})
	if err != nil {
		slog.Error("Position push: cannot encode a position", "error", err)
		return
	}
	if err := p.pubsub.Publish(Topic(userID), data); err != nil {
		slog.Error("Position push: cannot publish a position", "error", err)
	}
}
