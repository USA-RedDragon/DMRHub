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

package liveconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// Key is one setting of config.Config, named by its dotted path, such as
// dmr.ipsc.network-id. A list is one setting.
type Key struct {
	Path string
	// Setup is whether Setup changes the setting while DMRHub runs. The
	// other settings take effect at the start of DMRHub only, and come
	// from the config file, the environment and the flags alone.
	Setup bool
	// Secret is whether the config API never shows the setting.
	Secret bool
	index  []int
}

// setupPaths are the settings that Setup changes, with all the settings
// under them. DMRHub puts each into effect while it runs.
func setupPaths() []string {
	return []string{
		"log-level",
		"network-name",
		"auto-approve-users",
		"http.robots-txt",
		"http.canonical-host",
		"smtp.enabled",
		"smtp.host",
		"smtp.port",
		"smtp.tls",
		"smtp.username",
		"smtp.from",
		"smtp.auth-method",
		"dmr.mmdvm",
		"dmr.openbridge",
		"dmr.ipsc",
		"dmr.modem",
		"dmr.disable-radio-id-validation",
		"dmr.radio-id-url",
		"dmr.repeater-id-url",
		"dmr.public-address",
		"dmr.retention",
		"dmr.positions",
		"dmr.plugins.in-process",
		"dmr.transcoding",
		"dmr.private-calls",
		"dmr.drain-timeout",
		"dmr.ip-lists",
		"dmr.routing",
		"dmr.repeater-quota",
	}
}

// bootPaths are the settings that take effect at the start of DMRHub
// only: the secrets, the database and Redis that hold the other
// settings, and the listeners that start once.
func bootPaths() []string {
	return []string{
		"secret",
		"password-salt",
		"hibp-api-key",
		"redis",
		"database",
		"http.bind",
		"http.port",
		"http.trusted-proxies",
		"http.cors",
		"smtp.password",
		"metrics",
		"pprof",
		"dmr.plugins.enabled",
		"dmr.plugins.bind",
		"dmr.plugins.port",
		"dmr.plugins.cert-file",
		"dmr.plugins.key-file",
	}
}

func under(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+".") || strings.HasPrefix(path, prefix+"[")
}

func underAny(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if under(path, p) {
			return true
		}
	}
	return false
}

// Keys returns every setting of config.Config, in the order of its fields.
func Keys() []Key {
	var out []Key
	walk(reflect.TypeFor[config.Config](), "", nil, &out)
	return out
}

func walk(t reflect.Type, prefix string, index []int, out *[]Key) {
	for i := range t.NumField() {
		f := t.Field(i)
		name := f.Tag.Get("name")
		if name == "" || !f.IsExported() {
			continue
		}
		path := prefix + name
		idx := append(append([]int{}, index...), i)
		if f.Type.Kind() == reflect.Struct {
			walk(f.Type, path+".", idx, out)
			continue
		}
		*out = append(*out, Key{
			Path:   path,
			Setup:  underAny(path, setupPaths()) && !underAny(path, bootPaths()),
			Secret: f.Tag.Get("json") == "-",
			index:  idx,
		})
	}
}

// Classified reports whether path is in exactly one of the Setup and boot
// lists, so that a new setting cannot be added without a choice.
func Classified(path string) bool {
	return underAny(path, setupPaths()) != underAny(path, bootPaths())
}

func (k Key) field(c *config.Config) reflect.Value {
	return reflect.ValueOf(c).Elem().FieldByIndex(k.index)
}

// Value returns the value of k in c as JSON. An empty list is [].
func (k Key) Value(c *config.Config) (json.RawMessage, error) {
	v := k.field(c)
	if v.Kind() == reflect.Slice && v.Len() == 0 {
		return json.RawMessage("[]"), nil
	}
	b, err := json.Marshal(v.Interface())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", k.Path, err)
	}
	return b, nil
}

// Set sets k in c to the JSON value raw.
func (k Key) Set(c *config.Config, raw json.RawMessage) error {
	v := k.field(c)
	p := reflect.New(v.Type())
	if err := json.Unmarshal(raw, p.Interface()); err != nil {
		return fmt.Errorf("%s: %w", k.Path, err)
	}
	v.Set(p.Elem())
	return nil
}

// Copy sets k in dst to its value in src.
func (k Key) Copy(dst, src *config.Config) {
	k.field(dst).Set(k.field(src))
}

// Equal reports whether k has the same value in a and b.
func (k Key) Equal(a, b *config.Config) bool {
	av, aerr := k.Value(a)
	bv, berr := k.Value(b)
	return aerr == nil && berr == nil && bytes.Equal(av, bv)
}
