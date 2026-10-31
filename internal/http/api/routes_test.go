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

package api

import "testing"

func TestSitemapURL(t *testing.T) {
	t.Parallel()
	for host, want := range map[string]string{
		"https://dmrhub.net":          "https://dmrhub.net/sitemap.xml",
		"https://dmrhub.net/":         "https://dmrhub.net/sitemap.xml",
		"http://hub.example.com:3005": "http://hub.example.com:3005/sitemap.xml",
		"dmrhub.example.com":          "https://dmrhub.example.com/sitemap.xml",
		"https://example.com/hub/":    "https://example.com/hub/sitemap.xml",
	} {
		if got := sitemapURL(host); got != want {
			t.Errorf("sitemapURL(%q) = %q, want %q", host, got, want)
		}
	}
}
