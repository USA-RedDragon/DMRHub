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
	"github.com/USA-RedDragon/configulator/v2"
)

// Source is where the value of a setting that a replica runs with comes
// from.
type Source string

const (
	SourceDefault Source = "default"
	SourceSetup   Source = "setup"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
	// SourceGenerated is a secret that DMRHub made because no deployment
	// set it.
	SourceGenerated Source = "generated"
)

// Lock is a setting that the config file, the environment or a flag sets.
// It wins over the value saved in Setup.
type Lock struct {
	Source Source
	// File is the config file that sets it, for SourceFile.
	File string
}

// Locks are the locked settings of a replica, by path.
type Locks map[string]Lock

func sourceOf(l configulator.Layer) Source {
	switch l {
	case configulator.LayerDefault:
		return SourceDefault
	case configulator.LayerFile:
		return SourceFile
	case configulator.LayerEnv:
		return SourceEnv
	case configulator.LayerCLI:
		return SourceFlag
	}
	return SourceDefault
}

func rank(s Source) int {
	switch s {
	case SourceDefault, SourceSetup, SourceGenerated:
		return 0
	case SourceFile:
		return 1
	case SourceEnv:
		return 2
	case SourceFlag:
		return 3
	}
	return 0
}

// LocksFrom returns the settings that the config file, the environment or
// the flags of report set. A list is locked as a whole when any of it is
// set.
func LocksFrom(report *configulator.Report) Locks {
	locks := Locks{}
	if report == nil {
		return locks
	}
	keys := Keys()
	for _, p := range report.Paths() {
		o, ok := report.Origin(p)
		if !ok || o.Layer == configulator.LayerDefault {
			continue
		}
		for _, k := range keys {
			if !under(p, k.Path) {
				continue
			}
			l := Lock{Source: sourceOf(o.Layer)}
			if l.Source == SourceFile {
				l.File = o.Detail
			}
			if old, ok := locks[k.Path]; !ok || rank(l.Source) > rank(old.Source) {
				locks[k.Path] = l
			}
		}
	}
	return locks
}
