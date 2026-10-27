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
	"reflect"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

func configKeys(t reflect.Type, prefix string, keys map[string]bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		name := f.Tag.Get("name")
		if name == "" {
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Slice || ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		keys[prefix+name] = true
		if ft.Kind() == reflect.Struct {
			configKeys(ft, prefix+name+".", keys)
		}
	}
}

func TestValidationErrorsNameRealKeys(t *testing.T) {
	t.Parallel()
	keys := map[string]bool{}
	configKeys(reflect.TypeFor[config.Config](), "", keys)

	bad := config.Config{}
	bad.Database.Driver = config.DatabaseDriverPostgres
	bad.SMTP.Enabled = true
	bad.Redis.Enabled = true
	errs := bad.ValidateWithFields()
	if len(errs) == 0 {
		t.Fatal("an empty config is valid")
	}
	for _, e := range errs {
		field := e.Field
		if i := strings.Index(field, "["); i >= 0 {
			field = field[:i]
		}
		if !keys[field] {
			t.Errorf("validation error %q names %q, which is not a config key", e.Err, e.Field)
		}
	}
}
