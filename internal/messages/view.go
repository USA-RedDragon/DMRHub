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
	"encoding/hex"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"gorm.io/gorm"
)

type names struct {
	callsigns  map[uint]string
	talkgroups map[uint]string
}

func lookup(db *gorm.DB, msgs []models.Message) (names, error) {
	n := names{callsigns: map[uint]string{}, talkgroups: map[uint]string{}}
	var userIDs, tgIDs []uint
	for i := range msgs {
		userIDs = append(userIDs, msgs[i].SourceID)
		if msgs[i].IsGroup {
			tgIDs = append(tgIDs, msgs[i].DestinationID)
		} else {
			userIDs = append(userIDs, msgs[i].DestinationID)
		}
	}
	if len(userIDs) > 0 {
		var users []models.User
		if err := db.Select("id", "callsign").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return n, fmt.Errorf("could not find message users: %w", err)
		}
		for _, u := range users {
			n.callsigns[u.ID] = u.Callsign
		}
	}
	if len(tgIDs) > 0 {
		var tgs []models.Talkgroup
		if err := db.Select("id", "name").Where("id IN ?", tgIDs).Find(&tgs).Error; err != nil {
			return n, fmt.Errorf("could not find message talkgroups: %w", err)
		}
		for _, tg := range tgs {
			n.talkgroups[tg.ID] = tg.Name
		}
	}
	return n, nil
}

func (n names) callsign(id uint) string {
	if c, ok := n.callsigns[id]; ok && c != "" {
		return c
	}
	if u, ok := userdb.Get(id); ok {
		return u.Callsign
	}
	return ""
}

func response(r models.CallResponse) string {
	if r == models.CallResponseNone {
		return "none"
	}
	return string(r)
}

func (n names) user(m *models.Message) apimodels.Message {
	out := apimodels.Message{
		ID:             m.ID,
		ReceivedAt:     m.ReceivedAt.UTC(),
		SourceID:       m.SourceID,
		SourceCallsign: n.callsign(m.SourceID),
		DestinationID:  m.DestinationID,
		Group:          m.IsGroup,
		Format:         string(m.Format),
		Text:           m.Text,
		Confirmed:      m.Confirmed,
		Response:       response(m.Response),
		ChecksumOK:     m.ChecksumOK,
		Octets:         m.Octets,
	}
	if m.IsGroup {
		out.TalkgroupName = n.talkgroups[m.DestinationID]
	} else {
		out.DestinationCallsign = n.callsign(m.DestinationID)
	}
	return out
}

// UserView returns msgs as a user sees them.
func UserView(db *gorm.DB, msgs []models.Message) ([]apimodels.Message, error) {
	n, err := lookup(db, msgs)
	if err != nil {
		return nil, err
	}
	out := make([]apimodels.Message, len(msgs))
	for i := range msgs {
		out[i] = n.user(&msgs[i])
	}
	return out, nil
}

// AdminView returns msgs as an admin sees them, with the raw octets when
// raw is set.
func AdminView(db *gorm.DB, msgs []models.Message, raw bool) ([]apimodels.AdminMessage, error) {
	n, err := lookup(db, msgs)
	if err != nil {
		return nil, err
	}
	out := make([]apimodels.AdminMessage, len(msgs))
	for i := range msgs {
		m := &msgs[i]
		out[i] = apimodels.AdminMessage{
			Message:        n.user(m),
			TransmissionID: m.TransmissionID,
			RepeaterID:     m.RepeaterID,
			Slot:           m.Slot,
			Transport:      m.Transport,
			StreamID:       m.StreamID,
			SAP:            m.SAP,
			BadCRC:         m.BadCRC,
			Blocks:         m.Blocks,
		}
		if raw {
			h := hex.EncodeToString(m.Raw)
			out[i].Raw = &h
		}
	}
	return out, nil
}
