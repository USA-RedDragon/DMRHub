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

package messages

import (
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"gorm.io/gorm"
)

const topicPrefix = "messages:"

// Topic is the pubsub topic of the messages pushed to the user userID.
func Topic(userID uint) string {
	return topicPrefix + strconv.FormatUint(uint64(userID), 10)
}

// Pusher publishes each new or changed message to the users who see it in
// a box, on Topic of each. It decides with models.MessageBoxes, the same
// check as the REST routes, at the time it publishes.
type Pusher struct {
	db     *gorm.DB
	pubsub pubsub.PubSub
}

// NewPusher returns a Pusher; give its Push to Store.SetNotify.
func NewPusher(db *gorm.DB, ps pubsub.PubSub) *Pusher {
	return &Pusher{db: db, pubsub: ps}
}

// Push publishes msg to the users who see it.
func (p *Pusher) Push(msg *models.Message) {
	if msg == nil {
		return
	}
	ids := []uint{msg.SourceID}
	if !msg.IsGroup && msg.DestinationID != msg.SourceID {
		ids = append(ids, msg.DestinationID)
	}
	done := map[uint]bool{}
	for _, id := range ids {
		owner, ok, err := models.RadioIDOwner(p.db, id)
		if err != nil {
			slog.Error("Message push: cannot find the owner of a radio ID", "id", id, "error", err)
			continue
		}
		if !ok || done[owner] {
			continue
		}
		done[owner] = true
		p.pushTo(owner, msg)
	}
}

func (p *Pusher) pushTo(userID uint, msg *models.Message) {
	var user models.User
	if err := p.db.First(&user, userID).Error; err != nil {
		slog.Error("Message push: cannot load a user", "user", userID, "error", err)
		return
	}
	if !user.SMSStorageEnabled || user.Suspended || !user.Approved {
		return
	}
	boxes, err := models.MessageBoxes(p.db, &user, msg.ID)
	if err != nil {
		slog.Error("Message push: cannot find the boxes of a message", "error", err)
		return
	}
	if len(boxes) == 0 {
		return
	}
	view, err := UserView(p.db, []models.Message{*msg})
	if err != nil {
		slog.Error("Message push: cannot show a message", "error", err)
		return
	}
	frame := apimodels.MessagePush{Message: view[0]}
	for _, b := range boxes {
		frame.Boxes = append(frame.Boxes, string(b))
	}
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("Message push: cannot encode a message", "error", err)
		return
	}
	if err := p.pubsub.Publish(Topic(userID), data); err != nil {
		slog.Error("Message push: cannot publish a message", "error", err)
	}
}
