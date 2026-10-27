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

package config_test

import (
	"errors"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/configulator/v2"
)

func TestPrivateCallDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	want := config.PrivateRoute{Reach: config.PrivateReachHybrid, Unknown: config.PrivateUnknownDrop}
	if got := cfg.DMR.PrivateCalls.Route(); got != want {
		t.Errorf("voice route %+v, want %+v", got, want)
	}
	if got := cfg.DMR.PrivateCalls.DataRoute(); got != want {
		t.Errorf("data route %+v, want %+v", got, want)
	}
	if got := (config.PrivateCalls{}).Route(); got != want {
		t.Errorf("empty voice route %+v, want %+v", got, want)
	}
}

func TestPrivateCallDataTakesTheVoiceSettings(t *testing.T) {
	t.Parallel()
	voice := config.PrivateRoute{Reach: config.PrivateReachAnyone, Unknown: config.PrivateUnknownFlood, MaxAge: time.Hour}
	for _, c := range []struct {
		data config.PrivateData
		want config.PrivateRoute
	}{
		{config.PrivateData{}, voice},
		{config.PrivateData{Reach: config.PrivateReachMembers}, config.PrivateRoute{Reach: config.PrivateReachMembers, Unknown: config.PrivateUnknownFlood, MaxAge: time.Hour}},
		{config.PrivateData{Unknown: config.PrivateUnknownDrop}, config.PrivateRoute{Reach: config.PrivateReachAnyone, Unknown: config.PrivateUnknownDrop, MaxAge: time.Hour}},
		{config.PrivateData{MaxAge: 5 * time.Minute}, config.PrivateRoute{Reach: config.PrivateReachAnyone, Unknown: config.PrivateUnknownFlood, MaxAge: 5 * time.Minute}},
		{config.PrivateData{MaxAge: -time.Second}, config.PrivateRoute{Reach: config.PrivateReachAnyone, Unknown: config.PrivateUnknownFlood}},
	} {
		if got := (config.PrivateCalls{Voice: voice, Data: c.data}).DataRoute(); got != c.want {
			t.Errorf("data %+v: route %+v, want %+v", c.data, got, c.want)
		}
	}
}

func TestPrivateCallValidate(t *testing.T) {
	t.Parallel()
	cfg := makeValidConfig()
	cfg.DMR.PrivateCalls = config.PrivateCalls{
		Voice: config.PrivateRoute{Reach: "everyone", Unknown: "broadcast", MaxAge: -time.Second},
		Data:  config.PrivateData{Reach: "nobody", Unknown: "maybe", MaxAge: -time.Second},
	}
	fields := map[string]error{}
	for _, e := range cfg.ValidateWithFields() {
		fields[e.Field] = e.Err
	}
	for field, want := range map[string]error{
		"dmr.private-calls.voice.reach":   config.ErrInvalidPrivateReach,
		"dmr.private-calls.voice.unknown": config.ErrInvalidPrivateUnknown,
		"dmr.private-calls.voice.max-age": config.ErrInvalidPrivateMaxAge,
		"dmr.private-calls.data.reach":    config.ErrInvalidPrivateReach,
		"dmr.private-calls.data.unknown":  config.ErrInvalidPrivateUnknown,
	} {
		if !errors.Is(fields[field], want) {
			t.Errorf("%s: error %v, want %v", field, fields[field], want)
		}
	}
	if err, ok := fields["dmr.private-calls.data.max-age"]; ok {
		t.Errorf("a negative data max age is no limit, got %v", err)
	}
	cfg.DMR.PrivateCalls = config.PrivateCalls{
		Voice: config.PrivateRoute{Reach: config.PrivateReachMembers, Unknown: config.PrivateUnknownFlood, MaxAge: time.Hour},
		Data:  config.PrivateData{Reach: config.PrivateReachAnyone},
	}
	if err := cfg.DMR.PrivateCalls.Validate(); err != nil {
		t.Errorf("valid private call settings: %v", err)
	}
}
