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

// AdminPlugin is a plugin as admins see it.
type AdminPlugin struct {
	ID             uint          `json:"id"`
	Name           string        `json:"name"`
	Enabled        bool          `json:"enabled"`
	Ranges         []PluginRange `json:"ranges"`
	Online         bool          `json:"online"`
	ConnectedSince *time.Time    `json:"connected_since"`
	LastSeenAt     *time.Time    `json:"last_seen_at"`
	Address        string        `json:"address"`
	TokenCreatedAt time.Time     `json:"token_created_at"`
	CallsToday     int64         `json:"calls_today"`
}

// NewAdminPlugin returns the admin form of p at now.
func NewAdminPlugin(p *models.Plugin, callsToday int64, now time.Time) AdminPlugin {
	out := AdminPlugin{
		ID: p.ID, Name: p.Name, Enabled: p.Enabled, Ranges: make([]PluginRange, len(p.Ranges)), Online: p.Online(now),
		LastSeenAt: p.LastSeenAt, TokenCreatedAt: p.TokenCreatedAt, CallsToday: callsToday,
	}
	for i, r := range p.Ranges {
		out.Ranges[i] = PluginRange{First: r.First, Last: r.Last}
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

// PluginList is the plugins, by name, and how they reach the hub.
type PluginList struct {
	Plugins    []AdminPlugin    `json:"plugins"`
	Connection PluginConnection `json:"connection"`
}

// PluginPost adds a plugin.
type PluginPost struct {
	Name   string        `json:"name" binding:"required"`
	Ranges []PluginRange `json:"ranges"`
}

// PluginPut changes a plugin.
type PluginPut struct {
	Name    string        `json:"name" binding:"required"`
	Enabled *bool         `json:"enabled" binding:"required"`
	Ranges  []PluginRange `json:"ranges"`
}

// PluginToken is a plugin with its token, which is shown only once.
type PluginToken struct {
	Plugin AdminPlugin `json:"plugin"`
	Token  string      `json:"token"`
}
