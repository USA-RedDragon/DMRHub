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

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

type shotList struct {
	Start    time.Time `yaml:"start"`
	TimeZone string    `yaml:"timezone"`
	Basemap  struct {
		Key       string `yaml:"key"`
		PartBytes int64  `yaml:"part_bytes"`
	} `yaml:"basemap"`
	Shots []shot `yaml:"shots"`
}

type shot struct {
	Name   string        `yaml:"name"`
	Phase  string        `yaml:"phase"`
	As     string        `yaml:"as"`
	Route  string        `yaml:"route"`
	Wait   string        `yaml:"wait"`
	Needs  []string      `yaml:"needs"`
	Steps  []string      `yaml:"steps"`
	After  []string      `yaml:"after"`
	Clip   string        `yaml:"clip"`
	Keep   bool          `yaml:"keep"`
	Frozen bool          `yaml:"frozen"`
	Pause  time.Duration `yaml:"pause"`
}

func loadList(path string) (*shotList, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read the shot list: %w", err)
	}
	var l shotList
	if err := yaml.UnmarshalWithOptions(data, &l, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("bad shot list %s: %w", path, err)
	}
	if l.Start.IsZero() || l.TimeZone == "" || l.Basemap.Key == "" {
		return nil, fmt.Errorf("%s needs start, timezone and basemap", path)
	}
	seen := map[string]bool{}
	for _, s := range l.Shots {
		switch {
		case !strings.HasSuffix(s.Name, ".png") || strings.Contains(s.Name, "/"):
			return nil, fmt.Errorf("shot %q: the name must be a file name that ends in .png", s.Name)
		case seen[s.Name]:
			return nil, fmt.Errorf("shot %s is in the list twice", s.Name)
		case s.Phase != "setup" && s.Phase != "main":
			return nil, fmt.Errorf("shot %s: phase must be setup or main", s.Name)
		}
		seen[s.Name] = true
	}
	return &l, nil
}

func (l *shotList) names() []string {
	out := make([]string, 0, len(l.Shots))
	for _, s := range l.Shots {
		out = append(out, s.Name)
	}
	return out
}

var imageRef = regexp.MustCompile(`Screenshots/([A-Za-z0-9._-]+\.png)`)

const placeholder = "<!-- screenshot:"

func docFiles(root string) ([]string, error) {
	files := []string{filepath.Join(root, "README.md")}
	err := filepath.WalkDir(filepath.Join(root, "doc"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not list the docs: %w", err)
	}
	return files, nil
}

func checkDocs(root string, l *shotList) error {
	files, err := docFiles(root)
	if err != nil {
		return err
	}
	made := l.names()
	used := map[string]bool{}
	var problems []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", f, err)
		}
		rel, _ := filepath.Rel(root, f)
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, placeholder) {
				problems = append(problems, fmt.Sprintf("%s:%d: a screenshot placeholder is left", rel, i+1))
			}
			for _, m := range imageRef.FindAllStringSubmatch(line, -1) {
				used[m[1]] = true
				if !slices.Contains(made, m[1]) {
					problems = append(problems, fmt.Sprintf("%s:%d: %s is not in the shot list", rel, i+1, m[1]))
				}
			}
		}
	}
	for _, n := range made {
		if !used[n] {
			problems = append(problems, fmt.Sprintf("shot %s is in the shot list, but no doc uses it", n))
		}
	}
	dir := filepath.Join(root, "doc", "Screenshots")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("could not list %s: %w", dir, err)
	}
	for _, e := range entries {
		if !slices.Contains(made, e.Name()) {
			problems = append(problems, fmt.Sprintf("doc/Screenshots/%s is not in the shot list", e.Name()))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("the docs and the shot list do not agree:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}
