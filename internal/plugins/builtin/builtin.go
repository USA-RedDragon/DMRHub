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

// Package builtin holds the plugins that come with DMRHub, built for
// wasip1, to run inside DMRHub: the parrot and the location server.
package builtin

//go:generate go run ./gen

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	"github.com/USA-RedDragon/DMRHub/plugins/parrot"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/klauspost/compress/zstd"
)

// maxModule is the largest module that a built-in plugin decompresses to.
const maxModule = 64 << 20

//go:embed parrot.wasm.zst
var parrotModule []byte

//go:embed locator.wasm.zst
var locatorModule []byte

// Plugin is a plugin that comes with DMRHub.
type Plugin struct {
	// Key names the plugin in the database.
	Key string
	// Name is the name it starts with.
	Name string
	// Ranges are the IDs it starts with.
	Ranges     []models.PluginRange
	compressed []byte
}

func of(key string, hello *pluginv1.Hello, compressed []byte) Plugin {
	p := Plugin{Key: key, Name: hello.GetName(), compressed: compressed}
	for _, r := range hello.GetIds() {
		p.Ranges = append(p.Ranges, models.PluginRange{First: uint(r.GetFirst()), Last: uint(r.GetLast())})
	}
	return p
}

// All returns the plugins that come with DMRHub.
func All() []Plugin {
	return []Plugin{
		of("parrot", parrot.Hello(), parrotModule),
		of("locator", locator.Hello(), locatorModule),
	}
}

// Module returns the WebAssembly module of p.
func (p Plugin) Module() ([]byte, error) {
	d, err := zstd.NewReader(bytes.NewReader(p.compressed), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, fmt.Errorf("could not read the module of %s: %w", p.Key, err)
	}
	defer d.Close()
	b, err := io.ReadAll(io.LimitReader(d, maxModule))
	if err != nil {
		return nil, fmt.Errorf("could not read the module of %s: %w", p.Key, err)
	}
	return b, nil
}
