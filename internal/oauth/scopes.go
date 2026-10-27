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

package oauth

import (
	"fmt"
	"slices"
	"strings"
)

// The scopes that an app can ask for.
const (
	ScopeTalkgroupsRead     = "talkgroups:read"
	ScopeLastHeardRead      = "lastheard:read"
	ScopeMessagesRead       = "messages:read"
	ScopePositionsRead      = "positions:read"
	ScopeHotspotsRead       = "hotspots:read"
	ScopeHotspotsRegister   = "hotspots:register"
	ScopeHotspotsTalkgroups = "hotspots:talkgroups"
)

// Scope is what an app can do with an account, in the words that the
// consent page and the list of apps show.
type Scope struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func scopes() []Scope {
	return []Scope{
		{ScopeTalkgroupsRead, "talkgroups", "See the talkgroups of this network"},
		{ScopeLastHeardRead, "last heard", "See last heard for your radio IDs and your hotspots"},
		{ScopeMessagesRead, "messages", "Read your text messages, and get new ones as they arrive"},
		{ScopePositionsRead, "positions", "See where your radios are, and the radios shared with you, as they report"},
		{ScopeHotspotsRead, "hotspots", "See your hotspots"},
		{ScopeHotspotsRegister, "hotspots", "Add hotspots to your account and see their passwords"},
		{ScopeHotspotsTalkgroups, "hotspot talkgroups", "Change the talkgroups of your hotspots"},
	}
}

// Scopes returns every scope, in the order the consent page shows them.
func Scopes() []Scope {
	return scopes()
}

// ParseScopes returns the scopes of the space-separated list s in the
// order of Scopes, once each. Every name must be known, and there must be
// at least one.
func ParseScopes(s string) ([]Scope, error) {
	names := strings.Fields(s)
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: ask for at least one scope", ErrInvalidScope)
	}
	all := scopes()
	for _, n := range names {
		if !slices.ContainsFunc(all, func(sc Scope) bool { return sc.Name == n }) {
			return nil, fmt.Errorf("%w: unknown scope %q", ErrInvalidScope, n)
		}
	}
	var out []Scope
	for _, sc := range all {
		if slices.Contains(names, sc.Name) {
			out = append(out, sc)
		}
	}
	return out, nil
}

// Summary returns the scopes of the space-separated list s as a short
// phrase, such as "Talkgroups, last heard".
func Summary(s string) string {
	parsed, err := ParseScopes(s)
	if err != nil {
		return ""
	}
	var labels []string
	for _, sc := range parsed {
		if !slices.Contains(labels, sc.Label) {
			labels = append(labels, sc.Label)
		}
	}
	out := strings.Join(labels, ", ")
	if out == "" {
		return out
	}
	return strings.ToUpper(out[:1]) + out[1:]
}
