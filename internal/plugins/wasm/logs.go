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

package wasm

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxLogLine is the longest line of a plugin's stderr that is logged.
const maxLogLine = 1024

// logWriter logs the lines that a plugin writes on its stderr, at most
// perSecond each second.
type logWriter struct {
	name      string
	perSecond int

	mu      sync.Mutex
	line    []byte
	second  time.Time
	logged  int
	dropped int
	last    string
	failure string
}

func newLogWriter(name string, perSecond int) *logWriter {
	return &logWriter{name: name, perSecond: perSecond}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rest := p
	for len(rest) > 0 {
		nl := bytes.IndexByte(rest, '\n')
		chunk := rest
		if nl >= 0 {
			chunk = rest[:nl]
		}
		if room := maxLogLine - len(w.line); room > 0 {
			w.line = append(w.line, chunk[:min(len(chunk), room)]...)
		}
		if nl < 0 {
			break
		}
		w.emit()
		rest = rest[nl+1:]
	}
	return len(p), nil
}

func (w *logWriter) emit() {
	now := time.Now()
	if now.Sub(w.second) >= time.Second {
		if w.dropped > 0 {
			slog.Warn("Plugins: dropped lines of a plugin that logs too much", "plugin", w.name, "lines", w.dropped)
		}
		w.second, w.logged, w.dropped = now, 0, 0
	}
	line := string(bytes.TrimRight(w.line, "\r"))
	w.line = w.line[:0]
	if strings.TrimSpace(line) != "" {
		w.last = line
	}
	if w.failure == "" && (strings.HasPrefix(line, "panic: ") || strings.HasPrefix(line, "fatal error: ")) {
		w.failure = line
	}
	if w.logged >= w.perSecond {
		w.dropped++
		return
	}
	w.logged++
	slog.Info("Plugins: plugin log", "plugin", w.name, "line", line)
}

// said returns what the plugin said last of why it stops: the panic or
// fatal error of the Go runtime, else the error of its last log line, else
// its last line.
func (w *logWriter) said() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != "" {
		return w.failure
	}
	return errorOf(w.last)
}

// errorOf returns the value of the error attribute of line, a line of
// log/slog's text format, or line when it has none.
func errorOf(line string) string {
	_, rest, ok := strings.Cut(line, " error=")
	if !ok {
		return line
	}
	if strings.HasPrefix(rest, `"`) {
		if v, err := strconv.QuotedPrefix(rest); err == nil {
			if u, err := strconv.Unquote(v); err == nil {
				return u
			}
		}
	}
	v, _, _ := strings.Cut(rest, " ")
	return v
}
