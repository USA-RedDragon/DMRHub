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

package mmdvm

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
)

// A browser hotspot moves to another replica before the one that serves it
// stops, without a gap and without a new login: the draining replica sends
// it a drain notice with a resume token, the browser opens a second
// session, which the load balancer takes to another replica, and sends the
// token there, and that replica takes the MMDVM session over at the new
// session. The browser then sends on the new session and closes the old
// one. The token is signed with a key that the replicas derive from the
// secret that they share, so any of them can check it.
const (
	// resumeTTL is how long a resume token is good for.
	resumeTTL = time.Minute
	// resumeMACLen is how many octets of the HMAC a token holds.
	resumeMACLen = 16
	// noticeWait is the longest time the drain waits for the notices to
	// go out.
	noticeWait        = 2 * time.Second
	spentResumePrefix = "dmrhub:wt:resumed:"
)

// wtDrain is the message on the stream that a draining replica opens.
type wtDrain struct {
	Drain  bool   `json:"drain"`
	Resume string `json:"resume"`
}

// wtResume is the message of a browser that resumes its session, and the
// answer of the replica.
type wtResume struct {
	Resume  string `json:"resume,omitempty"`
	Resumed bool   `json:"resumed"`
	Reason  string `json:"reason,omitempty"`
}

func resumeKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("dmrhub webtransport resume"), secret...))
	return sum[:]
}

// resumeToken returns a token that lets the session of the repeater id
// move to another WebTransport session until resumeTTL after now.
func (s *Server) resumeToken(id uint32, now time.Time) string {
	var b [12 + resumeMACLen]byte
	binary.BigEndian.PutUint32(b[:4], id)
	binary.BigEndian.PutUint64(b[4:12], uint64(now.Add(resumeTTL).Unix())) //nolint:gosec // a time after 1970
	mac := hmac.New(sha256.New, s.resumeKey)
	mac.Write(b[:12])
	copy(b[12:], mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// checkResumeToken returns the repeater of a token that is good at now.
func (s *Server) checkResumeToken(token string, now time.Time) (uint32, bool) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 12+resumeMACLen {
		return 0, false
	}
	mac := hmac.New(sha256.New, s.resumeKey)
	mac.Write(b[:12])
	if !hmac.Equal(mac.Sum(nil)[:resumeMACLen], b[12:]) {
		return 0, false
	}
	if int64(binary.BigEndian.Uint64(b[4:12])) < now.Unix() { //nolint:gosec // a time after 1970
		return 0, false
	}
	return binary.BigEndian.Uint32(b[:4]), true
}

// DrainWebTransport refuses new WebTransport sessions and tells each
// browser hotspot of this replica to move to another one.
func (s *Server) DrainWebTransport(ctx context.Context) {
	if s.wt == nil {
		return
	}
	s.wt.draining.Store(true)
	now := time.Now()
	type notice struct {
		addr wtAddr
		msg  wtDrain
	}
	var notices []notice
	s.mu.Lock()
	for id, l := range s.byID {
		if a, ok := l.ep.addr.(wtAddr); ok && l.ep.transport == webTransportName {
			notices = append(notices, notice{addr: a, msg: wtDrain{Drain: true, Resume: s.resumeToken(id, now)}})
		}
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, noticeWait)
	defer cancel()
	var wg sync.WaitGroup
	for _, n := range notices {
		wg.Go(func() {
			if err := s.wt.notify(ctx, n.addr, n.msg); err != nil {
				slog.Warn("Error telling a browser hotspot to move", "session", n.addr, "error", err)
			}
		})
	}
	wg.Wait()
	slog.Info("Told the browser hotspots to move to another replica", "count", len(notices))
}

// spendToken reports whether token was not used before, and marks it used
// for as long as it is good, so that a token moves a session once.
func (s *Server) spendToken(ctx context.Context, token string) bool {
	sum := sha256.Sum256([]byte(token))
	key := spentResumePrefix + base64.RawURLEncoding.EncodeToString(sum[:])
	fresh, err := s.kv.SetNX(ctx, key, "1", resumeTTL)
	return err == nil && fresh
}

// resumeWebTransport takes over, at the WebTransport session addr, the
// MMDVM session that the token names, as a browser hotspot that moves from
// a draining replica asks.
func (s *Server) resumeWebTransport(addr wtAddr, token string) wtResume {
	now := time.Now()
	id, ok := s.checkResumeToken(token, now)
	if !ok {
		return wtResume{Reason: "bad token"}
	}
	ctx := context.Background()
	repeater, err := s.kvClient.GetRepeater(ctx, uint(id))
	if err != nil || repeater.Connection != models.RepeaterStateConnected || repeater.Port != 0 ||
		!strings.HasPrefix(repeater.IP, webTransportName+"/") {
		return wtResume{Reason: "no session"}
	}
	if s.hub == nil || (!s.hub.OwnsRepeater(ctx, uint(id)) && !s.hub.TakeOver(ctx, uint(id))) {
		return wtResume{Reason: "the session is not handed over"}
	}
	if !s.spendToken(ctx, token) {
		return wtResume{Reason: "the token was used"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped.Load() {
		return wtResume{Reason: "stopping"}
	}
	ep := endpoint{transport: webTransportName, addr: addr}
	l, err := s.linkAt(ep, dmrd.DialectMMDVM)
	if err != nil || l.m.State() != dmrdsession.StateIdle {
		return wtResume{Reason: "busy"}
	}
	l.heard = now
	s.resumeLink(ctx, s.now(), l, id)
	if s.byID[id] != l {
		s.forget(l)
		return wtResume{Reason: "not resumed"}
	}
	repeater.IP, repeater.Port = ep.kvAddr()
	s.records.Do(uint(id), func() { s.kvClient.StoreRepeater(ctx, uint(id), repeater) })
	slog.Info("Browser hotspot moved from another replica", "repeaterID", id, "session", addr)
	return wtResume{Resumed: true}
}
