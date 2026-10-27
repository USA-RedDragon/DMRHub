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

package modem

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

const (
	statusPrefix  = "modem:status:"
	statusTTL     = 3 * pingWrite
	statusTimeout = time.Second
	statusScan    = 100
)

// Status is the live state of the modem of a replica.
type Status struct {
	// Replica is the replica that drives the modem.
	Replica string `json:"replica,omitempty"`
	Port    string `json:"port"`
	Baud    int    `json:"baud"`
	// Linked is whether the link to the board is up.
	Linked   bool     `json:"linked"`
	Firmware string   `json:"firmware,omitempty"`
	Protocol uint8    `json:"protocol,omitempty"`
	Modes    []string `json:"modes,omitempty"`
	// Since is when the link came up, or when the last open failed.
	Since time.Time `json:"since,omitzero"`
	// Error is why the last open of the board failed, while the link is
	// down.
	Error string `json:"error,omitempty"`
}

// Modes returns the names of the modes of c, DMR first.
func Modes(c mmdvm.Capabilities) []string {
	out := []string{}
	for _, m := range []struct {
		c    mmdvm.Capabilities
		name string
	}{
		{mmdvm.CapDMR, "DMR"}, {mmdvm.CapDStar, "D-Star"}, {mmdvm.CapYSF, "YSF"}, {mmdvm.CapP25, "P25"},
		{mmdvm.CapNXDN, "NXDN"}, {mmdvm.CapFM, "FM"}, {mmdvm.CapPOCSAG, "POCSAG"},
	} {
		if c.Has(m.c) {
			out = append(out, m.name)
		}
	}
	return out
}

// firmware returns the description of v up to the 0 octet that MMDVM_HS
// puts before its unique ID.
func firmware(v mmdvm.Version) string {
	d, _, _ := bytes.Cut(v.Description, []byte{0})
	return string(bytes.TrimSpace(d))
}

// Status returns the live state of the modem.
func (s *Server) Status() Status {
	s.stmu.Lock()
	defer s.stmu.Unlock()
	st := s.status
	st.Modes = append([]string(nil), st.Modes...)
	return st
}

func (s *Server) replica() string {
	return s.opts.Replica
}

func (s *Server) setLinked(v mmdvm.Version) {
	s.stmu.Lock()
	s.status = Status{Replica: s.replica(), Port: s.m.Port, Baud: s.m.Baud, Linked: true, Firmware: firmware(v),
		Protocol: v.Protocol, Modes: Modes(v.Capabilities), Since: time.Now()}
	s.stmu.Unlock()
	s.storeStatus()
}

func (s *Server) setDown(err error) {
	s.stmu.Lock()
	s.status = Status{Replica: s.replica(), Port: s.m.Port, Baud: s.m.Baud, Since: time.Now()}
	if err != nil {
		s.status.Error = err.Error()
	}
	s.stmu.Unlock()
	s.storeStatus()
}

// storeStatus writes the live state to the KV store, where the other
// replicas read it, for statusTTL.
func (s *Server) storeStatus() {
	store, id := s.opts.KV, s.replica()
	if store == nil || id == "" {
		return
	}
	st := s.Status()
	s.jobs.Do(uint(s.id), func() {
		b, err := json.Marshal(st)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(s.ctx, statusTimeout)
		defer cancel()
		key := statusPrefix + id
		if err := store.Set(ctx, key, b); err != nil {
			slog.Error("Could not store the state of the modem", "error", err)
			return
		}
		if err := store.Expire(ctx, key, statusTTL); err != nil {
			slog.Error("Could not set the lifetime of the state of the modem", "error", err)
		}
	})
}

func (s *Server) dropStatus(ctx context.Context) {
	store, id := s.opts.KV, s.replica()
	if store == nil || id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusTimeout)
	defer cancel()
	if err := store.Delete(ctx, statusPrefix+id); err != nil {
		slog.Error("Could not remove the state of the modem", "error", err)
	}
}

// Statuses returns the live states of the modems of every replica in
// store.
func Statuses(ctx context.Context, store kv.KV) []Status {
	out := []Status{}
	if store == nil {
		return out
	}
	var cursor uint64
	for {
		keys, next, err := store.Scan(ctx, cursor, statusPrefix+"*", statusScan)
		if err != nil {
			slog.Error("Could not list the states of the modems", "error", err)
			return out
		}
		for _, key := range keys {
			b, err := store.Get(ctx, key)
			if err != nil {
				continue
			}
			var st Status
			if json.Unmarshal(b, &st) == nil && st.Replica != "" {
				out = append(out, st)
			}
		}
		if next == 0 {
			return out
		}
		cursor = next
	}
}
