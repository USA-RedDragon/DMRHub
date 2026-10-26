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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// PluginRange is the DMR IDs from First to Last, both included.
type PluginRange struct {
	First uint `json:"first"`
	Last  uint `json:"last"`
}

// PluginHost is a host that a plugin inside DMRHub asks to reach, why, and
// whether an admin allowed it.
type PluginHost struct {
	Address string `json:"address"`
	Reason  string `json:"reason"`
	Allowed bool   `json:"allowed"`
}

// AdminPlugin is a plugin as admins see it. Kind is "external" for a
// plugin that runs as its own program, and "builtin" or "uploaded" for one
// that runs inside DMRHub.
type AdminPlugin struct {
	ID             uint          `json:"id"`
	Name           string        `json:"name"`
	Kind           string        `json:"kind"`
	Version        string        `json:"version"`
	Author         string        `json:"author"`
	Description    string        `json:"description"`
	Enabled        bool          `json:"enabled"`
	Ranges         []PluginRange `json:"ranges"`
	Hosts          []PluginHost  `json:"hosts"`
	Online         bool          `json:"online"`
	ConnectedSince *time.Time    `json:"connected_since"`
	LastSeenAt     *time.Time    `json:"last_seen_at"`
	Address        string        `json:"address"`
	TokenCreatedAt time.Time     `json:"token_created_at"`
	CallsToday     int64         `json:"calls_today"`
	// State is, for a plugin inside DMRHub, one of the PluginState values.
	State string `json:"state,omitempty"`
	// StoppedAt, RestartAt and StopReason say, for a plugin in
	// PluginStopped, when it stopped, when it starts again and why.
	StoppedAt  *time.Time `json:"stopped_at,omitempty"`
	RestartAt  *time.Time `json:"restart_at,omitempty"`
	StopReason string     `json:"stop_reason,omitempty"`
	// StopsLastHour is how many times a plugin inside DMRHub stopped by
	// itself in the last hour.
	StopsLastHour int `json:"stops_last_hour,omitempty"`
	// WaitingID and WaitingFor say, for a plugin in PluginWaiting, the ID it
	// waits for and the name of the plugin that has it.
	WaitingID  uint   `json:"waiting_id,omitempty"`
	WaitingFor string `json:"waiting_for,omitempty"`
}

// The states of a plugin inside DMRHub.
const (
	// PluginOff is a plugin that is off, or that runs inside DMRHub when
	// DMRHub runs no plugins inside.
	PluginOff = "off"
	// PluginRunning is a plugin that runs, on any replica.
	PluginRunning = "running"
	// PluginStarting is a plugin that is on and that DMRHub starts.
	PluginStarting = "starting"
	// PluginStopped is a plugin that stopped by itself and waits to start
	// again.
	PluginStopped = "stopped"
	// PluginWaiting is a built-in plugin that waits for its IDs.
	PluginWaiting = "waiting"
)

// NewAdminPlugin returns the admin form of p at now.
func NewAdminPlugin(p *models.Plugin, callsToday int64, now time.Time) AdminPlugin {
	out := AdminPlugin{
		ID: p.ID, Name: p.Name, Kind: p.Kind, Version: p.Version, Author: p.Author, Description: p.Description,
		Enabled: p.Enabled, Ranges: make([]PluginRange, len(p.Ranges)), Hosts: make([]PluginHost, len(p.Hosts)), Online: p.Online(now),
		LastSeenAt: p.LastSeenAt, TokenCreatedAt: p.TokenCreatedAt, CallsToday: callsToday,
	}
	for i, r := range p.Ranges {
		out.Ranges[i] = PluginRange{First: r.First, Last: r.Last}
	}
	for i, h := range p.Hosts {
		out.Hosts[i] = PluginHost{Address: h.Address, Reason: h.Reason, Allowed: h.Allowed}
	}
	if out.Online {
		out.ConnectedSince, out.Address = p.LastConnectedAt, p.LastAddress
	}
	return out
}

// PluginConnection is how plugins reach the hub.
type PluginConnection struct {
	Enabled        bool   `json:"enabled"`
	Address        string `json:"address"`
	Fingerprint    string `json:"fingerprint"`
	OwnCertificate bool   `json:"own_certificate"`
}

// PluginList is the plugins, by name, how they reach the hub, and whether
// plugins run inside DMRHub.
type PluginList struct {
	Plugins    []AdminPlugin    `json:"plugins"`
	Connection PluginConnection `json:"connection"`
	InProcess  bool             `json:"in_process"`
}

// PluginManifest is what an uploaded plugin says it is and asks for.
type PluginManifest struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Author      string        `json:"author"`
	Description string        `json:"description"`
	Ranges      []PluginRange `json:"ranges"`
	Hosts       []PluginHost  `json:"hosts"`
}

// PluginPost adds a plugin.
type PluginPost struct {
	Name   string        `json:"name" binding:"required"`
	Ranges []PluginRange `json:"ranges"`
}

// PluginPut changes a plugin. AllowedHosts, when set, are the hosts of a
// plugin inside DMRHub that it may reach.
type PluginPut struct {
	Name         string        `json:"name" binding:"required"`
	Enabled      *bool         `json:"enabled" binding:"required"`
	Ranges       []PluginRange `json:"ranges"`
	AllowedHosts *[]string     `json:"allowed_hosts"`
}

// PluginToken is a plugin with its token, which is shown only once.
type PluginToken struct {
	Plugin AdminPlugin `json:"plugin"`
	Token  string      `json:"token"`
}
