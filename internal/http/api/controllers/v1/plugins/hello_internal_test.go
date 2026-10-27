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

package plugins

import (
	"errors"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	dbpkg "github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEachProblemOfAHelloIsWorded(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg, "_pragma=foreign_keys(1)")
	db, err := dbpkg.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	_, _, err = models.CreatePlugin(db, "Weather", []models.PluginRange{{First: 9300, Last: 9300}}, time.Now())
	require.NoError(t, err)
	ok := func() *pluginv1.Hello {
		return &pluginv1.Hello{Name: "Net check-in", Version: "0.4.1", Ids: []*pluginv1.IDRange{{First: 9100, Last: 9100}}}
	}
	for name, c := range map[string]struct {
		hello *pluginv1.Hello
		want  []uploadProblem
	}{
		"fine":       {ok(), nil},
		"no name":    {func() *pluginv1.Hello { h := ok(); h.Name = ""; return h }(), []uploadProblem{{fieldFile, "The plugin's name must be 1 to 64 characters."}}},
		"long name":  {func() *pluginv1.Hello { h := ok(); h.Name = string(make([]byte, 65)); return h }(), []uploadProblem{{fieldFile, "The plugin's name must be 1 to 64 characters."}}},
		"no version": {func() *pluginv1.Hello { h := ok(); h.Version = ""; return h }(), []uploadProblem{{fieldFile, "The plugin gives no version."}}},
		"same name": {func() *pluginv1.Hello { h := ok(); h.Name = "Weather"; return h }(),
			[]uploadProblem{{fieldFile, "Another plugin is already called Weather."}}},
		"bad id": {func() *pluginv1.Hello { h := ok(); h.Ids = []*pluginv1.IDRange{{First: 0, Last: 0}}; return h }(),
			[]uploadProblem{{fieldIDs, "The plugin asks for ID 0, which is not a radio or talkgroup ID."}}},
		"taken id": {func() *pluginv1.Hello { h := ok(); h.Ids = []*pluginv1.IDRange{{First: 9290, Last: 9310}}; return h }(),
			[]uploadProblem{{fieldIDs, "9300 is already used by Weather. Change the IDs, or turn Weather off first."}}},
		"bad host": {func() *pluginv1.Hello { h := ok(); h.Hosts = []*pluginv1.Host{{Address: "example"}}; return h }(),
			[]uploadProblem{{fieldHosts, "The plugin asks to reach “example”, which is not a host and port such as nets.example.org:443."}}},
		"two problems": {func() *pluginv1.Hello {
			h := ok()
			h.Version = ""
			h.Hosts = []*pluginv1.Host{{Address: "example:0"}}
			return h
		}(),
			[]uploadProblem{{fieldFile, "The plugin gives no version."}, {fieldHosts, "The plugin asks to reach “example:0”, which is not a host and port such as nets.example.org:443."}}},
	} {
		info, ranges := infoOf(c.hello)
		assert.Equal(t, c.want, helloProblems(db, info, ranges, true), name)
	}
}

func TestEachStartProblemIsWorded(t *testing.T) {
	t.Parallel()
	l := wasm.DefaultLimits()
	for want, err := range map[string]error{
		"This file is not a WebAssembly module. Choose the .wasm file that the plugin's build made.":        wasm.ErrNotWasm,
		"This file is WebAssembly but not a DMRHub plugin. Plugins are built with GOOS=wasip1 GOARCH=wasm.": wasm.ErrNotPlugin,
		"The plugin did not introduce itself within 10 seconds. It must call plugin.Main when it starts.":   wasm.ErrNoHello,
		"The plugin stopped before it introduced itself: no config.":                                        &wasm.EndedError{Err: errors.New("exit_code(1)"), Said: "no config"},
		"The plugin stopped before it introduced itself: the plugin used too much CPU.":                     wasm.ErrCPU,
	} {
		assert.Equal(t, want, startProblem(err, l).Message)
	}
}
