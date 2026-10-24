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
package apimodels

import (
	"fmt"
	"math"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

type WSCallResponseUser struct {
	ID       uint   `json:"id"`
	Callsign string `json:"callsign"`
}

type WSCallResponseRepeater struct {
	RadioID  uint   `json:"id"`
	Callsign string `json:"callsign"`
}

type WSCallResponseTalkgroup struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type WSCallResponse struct {
	ID            uint                    `json:"id"`
	User          WSCallResponseUser      `json:"user"`
	StartTime     time.Time               `json:"start_time"`
	Duration      time.Duration           `json:"duration"`
	Active        bool                    `json:"active"`
	TimeSlot      bool                    `json:"time_slot"`
	GroupCall     bool                    `json:"group_call"`
	RepeaterID    uint                    `json:"repeater_id"`
	Repeater      WSCallResponseRepeater  `json:"repeater"`
	IsToTalkgroup bool                    `json:"is_to_talkgroup"`
	ToTalkgroup   WSCallResponseTalkgroup `json:"to_talkgroup"`
	IsToUser      bool                    `json:"is_to_user"`
	ToUser        WSCallResponseUser      `json:"to_user"`
	IsToRepeater  bool                    `json:"is_to_repeater"`
	ToRepeater    WSCallResponseRepeater  `json:"to_repeater"`
	DestinationID uint                    `json:"destination_id"`
	Loss          float32                 `json:"loss"`
	Jitter        float32                 `json:"jitter"`
	BER           float32                 `json:"ber"`
	RSSI          float32                 `json:"rssi"`
	Kind          models.CallKind         `json:"kind"`
	SubKind       string                  `json:"sub_kind"`
	SourceID      uint                    `json:"source_id"`
	Transport     string                  `json:"transport"`
	Bursts        uint                    `json:"bursts"`
	FID           *uint8                  `json:"fid"`
	Opcode        *uint8                  `json:"opcode"`
	SAP           *uint8                  `json:"sap"`
	DataFormat    string                  `json:"data_format"`
	Confirmed     bool                    `json:"confirmed"`
	Blocks        uint                    `json:"blocks"`
	Octets        uint                    `json:"octets"`
	BadCRC        uint                    `json:"bad_crc"`
	LostBlocks    uint                    `json:"lost_blocks"`
	Response      models.CallResponse     `json:"response"`
	TalkerAlias   string                  `json:"talker_alias"`
	AliasFormat   string                  `json:"alias_format"`
	Grid          string                  `json:"grid"`
	Latitude      *float64                `json:"latitude"`
	Longitude     *float64                `json:"longitude"`
	PositionError *uint8                  `json:"position_error"`
	Partial       bool                    `json:"partial"`
	Undeliverable []string                `json:"undeliverable"`
	Codec         string                  `json:"codec"`
	Codec2Mode    int                     `json:"codec2_mode"`
	SentAMBE      int                     `json:"sent_ambe"`
	SentCodec2    int                     `json:"sent_codec2"`
	MissedMS      int                     `json:"missed_ms"`
	Busy          bool                    `json:"busy"`
}

// NewWSCallResponseFromCall converts a models.Call into a WSCallResponse suitable
// for JSON serialisation over WebSocket or pubsub channels. It shows every
// field; NewWSCallResponsesFor shows the fields that a viewer may see.
func NewWSCallResponseFromCall(call *models.Call) WSCallResponse {
	resp := WSCallResponse{
		ID:            call.ID,
		StartTime:     call.StartTime,
		Duration:      call.Duration,
		Active:        call.Active,
		TimeSlot:      call.TimeSlot,
		GroupCall:     call.GroupCall,
		IsToTalkgroup: call.IsToTalkgroup,
		IsToUser:      call.IsToUser,
		IsToRepeater:  call.IsToRepeater,
		DestinationID: call.DestinationID,
		Loss:          call.Loss,
		Jitter:        call.Jitter,
		BER:           call.BER,
		RSSI:          call.RSSI,
		RepeaterID:    call.IngressID,
		Kind:          call.Kind,
		SubKind:       call.SubKind,
		SourceID:      call.SourceID,
		Transport:     call.Transport,
		Bursts:        call.Bursts,
		FID:           call.FID,
		Opcode:        call.Opcode,
		SAP:           call.SAP,
		DataFormat:    call.DataFormat,
		Confirmed:     call.Confirmed,
		Blocks:        call.Blocks,
		Octets:        call.Octets,
		BadCRC:        call.BadCRC,
		LostBlocks:    call.LostBlocks,
		Response:      call.Response,
		TalkerAlias:   call.TalkerAlias,
		AliasFormat:   call.AliasFormat,
		Latitude:      call.Latitude,
		Longitude:     call.Longitude,
		PositionError: call.PositionError,
		Partial:       call.Partial,
		Undeliverable: call.Undeliverable,
		Codec:         call.Codec,
		Codec2Mode:    call.Codec2Mode,
		SentAMBE:      call.SentAMBE,
		SentCodec2:    call.SentCodec2,
		MissedMS:      call.MissedMS,
		Busy:          call.TranscodeBusy,
	}
	if resp.Kind == "" {
		resp.Kind = models.CallKindVoice
	}
	resp.User.ID = call.User.ID
	resp.User.Callsign = call.User.Callsign
	if resp.User.ID == 0 {
		resp.User.ID = call.SourceID
	}
	if resp.SourceID == 0 {
		resp.SourceID = resp.User.ID
	}
	resp.Repeater.RadioID = call.Repeater.ID
	resp.Repeater.Callsign = call.Repeater.Callsign
	if resp.Repeater.RadioID == 0 {
		resp.Repeater.RadioID = call.IngressID
	}
	if resp.RepeaterID == 0 {
		resp.RepeaterID = resp.Repeater.RadioID
	}
	if call.Latitude != nil && call.Longitude != nil {
		resp.Grid = Maidenhead(*call.Latitude, *call.Longitude)
	}
	if call.IsToTalkgroup {
		resp.ToTalkgroup.ID = call.ToTalkgroup.ID
		resp.ToTalkgroup.Name = call.ToTalkgroup.Name
		resp.ToTalkgroup.Description = call.ToTalkgroup.Description
	}
	if call.IsToUser {
		resp.ToUser.ID = call.ToUser.ID
		resp.ToUser.Callsign = call.ToUser.Callsign
	}
	if call.IsToRepeater {
		resp.ToRepeater.RadioID = call.ToRepeater.ID
		resp.ToRepeater.Callsign = call.ToRepeater.Callsign
	}
	return resp
}

// MaskSMS shows an SMS row as plain data: it drops the sub kind, the SAP,
// the format, the size and the response, which would tell that the row
// carried a message.
func (r *WSCallResponse) MaskSMS() {
	if r.Kind != models.CallKindData || r.SubKind != models.SubKindSMS {
		return
	}
	r.SubKind, r.SAP, r.DataFormat, r.Confirmed, r.Response = "", nil, "", false, models.CallResponseNone
	r.Bursts, r.Blocks, r.Octets, r.BadCRC, r.LostBlocks = 0, 0, 0, 0, 0
}

// MaskData shows a data or UDT row as plain data with no size: it drops
// what MaskSMS drops, whatever the sub kind, and the duration, which tells
// the number of bursts.
func (r *WSCallResponse) MaskData() {
	if r.Kind != models.CallKindData && r.Kind != models.CallKindUDT {
		return
	}
	r.SubKind, r.SAP, r.DataFormat, r.Confirmed, r.Response = "", nil, "", false, models.CallResponseNone
	r.Bursts, r.Blocks, r.Octets, r.BadCRC, r.LostBlocks = 0, 0, 0, 0, 0
	r.Duration = 0
}

// HidePosition drops the coordinates of a GPS row and keeps its grid
// square.
func (r *WSCallResponse) HidePosition() {
	r.Latitude, r.Longitude = nil, nil
}

// Maidenhead returns the 4-character Maidenhead grid square of a position,
// such as EM12, or "" for a position out of range.
func Maidenhead(lat, lon float64) string {
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return ""
	}
	const fields, squares = "ABCDEFGHIJKLMNOPQR", "0123456789"
	x := math.Min(lon+180, 359.999999)
	y := math.Min(lat+90, 179.999999)
	return string([]byte{
		fields[int(x/20)],
		fields[int(y/10)],
		squares[int(math.Mod(x, 20)/2)],
		squares[int(math.Mod(y, 10))],
	})
}

// NewWSCallResponsesFor returns the rows of calls as viewer may see them. A
// nil viewer is a visitor who is not logged in. An admin sees every field.
// Another viewer sees a data or UDT row as plain data with no size unless
// it carried a message inside the window of window, and the coordinates
// only of their own GPS rows. A data row is masked by default, so that a
// response, a retry, or a row published before its message was stored
// tells nothing either.
func NewWSCallResponsesFor(db *gorm.DB, viewer *models.User, window models.SMSWindow, calls []models.Call) ([]WSCallResponse, error) {
	out := make([]WSCallResponse, 0, len(calls))
	admin := models.IsAdmin(viewer)
	var candidates []uint
	if !admin {
		for i := range calls {
			if calls[i].Kind == models.CallKindData || calls[i].Kind == models.CallKindUDT {
				candidates = append(candidates, calls[i].ID)
			}
		}
	}
	visible, err := models.VisibleSMSIDs(db, viewer, window, candidates)
	if err != nil {
		return nil, fmt.Errorf("failed to find the messages that the viewer may see: %w", err)
	}
	for i := range calls {
		resp := NewWSCallResponseFromCall(&calls[i])
		if !admin && !visible[resp.ID] {
			resp.MaskData()
		}
		if !admin && (viewer == nil || viewer.ID != resp.SourceID) {
			resp.HidePosition()
		}
		out = append(out, resp)
	}
	return out, nil
}
