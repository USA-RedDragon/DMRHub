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

import "time"

// UserSettings is the settings of the logged-in user. It never holds the
// time SMS Storage was turned on.
type UserSettings struct {
	SMSStorage     bool `json:"sms_storage"`
	SharePositions bool `json:"share_positions"`
	StoreForward   bool `json:"store_forward"`
	// HoldDays is how many days store and forward keeps a text.
	HoldDays int `json:"hold_days"`
	// SpokenPrompts reports that the hub speaks prompts to radios.
	SpokenPrompts bool `json:"spoken_prompts"`
	// PromptVoice is the voice that the user picked for the prompts, or
	// empty for the default voice of the hub, PromptDefault.
	PromptVoice   string `json:"prompt_voice"`
	PromptDefault string `json:"prompt_default"`
}

// PromptVoicePut sets the voice of the prompts of a user, or the default
// voice of the hub with an empty Voice.
type PromptVoicePut struct {
	Voice *string `json:"voice" binding:"required"`
}

// StoreForwardPut turns store and forward on or off.
type StoreForwardPut struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// HeldText is a text that store and forward keeps for one of the user's
// radios. FormatKnown is false while the hub has not heard the radio send
// a text whose format it can build, so it cannot send the text yet.
type HeldText struct {
	ID             uint      `json:"id"`
	MessageID      uint      `json:"message_id"`
	Text           string    `json:"text"`
	SourceID       uint      `json:"source_id"`
	SourceCallsign string    `json:"source_callsign"`
	RadioID        uint      `json:"radio_id"`
	HeldAt         time.Time `json:"held_at"`
	FormatKnown    bool      `json:"format_known"`
}

// HeldTextList is the texts held for the user's radios, oldest first.
type HeldTextList struct {
	Held []HeldText `json:"held"`
}

// SMSStoragePut turns SMS Storage on or off.
type SMSStoragePut struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// SharePositionsPut turns sharing of the user's positions on or off.
type SharePositionsPut struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

// RadioID is a subscriber radio ID that a user owns. Account is true for
// the account ID, which has no VerifiedAt.
type RadioID struct {
	ID         uint       `json:"id"`
	Callsign   string     `json:"callsign"`
	Account    bool       `json:"account"`
	VerifiedAt *time.Time `json:"verified_at"`
}

// RadioIDList is the radio IDs of a user, the account ID first.
type RadioIDList struct {
	RadioIDs []RadioID `json:"radio_ids"`
}

// RadioIDPost adds a radio ID.
type RadioIDPost struct {
	ID uint `json:"id" binding:"required"`
}

// PluginSwitch is a switch that a plugin declares, as the Settings page
// shows it to the logged-in user. Details holds the user's values.
type PluginSwitch struct {
	PluginID    uint                 `json:"plugin_id"`
	Plugin      string               `json:"plugin"`
	Key         string               `json:"key"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Notice      string               `json:"notice"`
	Details     []PluginSwitchDetail `json:"details"`
	Choice      *PluginSwitchChoice  `json:"choice"`
	On          bool                 `json:"on"`
}

// PluginSwitchDetail is a line of the card of a switch.
type PluginSwitchDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// PluginSwitchChoice is the choice of a switch and the user's option.
type PluginSwitchChoice struct {
	Label   string   `json:"label"`
	Options []string `json:"options"`
	Value   string   `json:"value"`
}

// PluginSwitchPut turns a switch of a plugin on or off, with an option of
// its choice, or the preset when empty.
type PluginSwitchPut struct {
	On     *bool  `json:"on" binding:"required"`
	Choice string `json:"choice"`
}
