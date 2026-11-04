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

package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/http/websocket"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	coderWebsocket "github.com/coder/websocket"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The reasons of the close frames of a listen session.
const (
	ListenRefusedOff      = "listening is off on this network"
	ListenRefusedFull     = "too many listeners, try again later"
	ListenRefusedAddress  = "too many listeners from your address"
	ListenRefusedTransmit = "listen-only session: it cannot transmit"
)

// The types of the text messages of a listen session.
const (
	listenTypeHello = "hello"
	listenTypeTuned = "tuned"
	listenTypeCall  = "call"
)

// The errors of a tune that the hub did not take.
const (
	listenErrNoTalkgroup = "this talkgroup is not open to listeners"
	listenErrLookup      = "the hub could not look the talkgroup up"
)

// ListenHello is the first message of a listen session: the network and
// the talkgroups that a visitor may tune to.
type ListenHello struct {
	Type       string                   `json:"type"`
	Network    string                   `json:"network"`
	Talkgroups []models.ListenTalkgroup `json:"talkgroups"`
}

// ListenTune is a message of the browser that tunes the session to a
// talkgroup, or to none with 0.
type ListenTune struct {
	Tune *uint `json:"tune"`
}

// ListenTuned answers a ListenTune: the talkgroup that the session hears
// now, 0 for none, and why the hub did not take the tune.
type ListenTuned struct {
	Type      string `json:"type"`
	Talkgroup uint   `json:"talkgroup"`
	Error     string `json:"error,omitempty"`
}

// ListenCall is a call on the talkgroup of a listen session, as the last
// heard page shows it to a visitor.
type ListenCall struct {
	Type      string                           `json:"type"`
	Talkgroup uint                             `json:"talkgroup"`
	Source    uint                             `json:"source"`
	Callsign  string                           `json:"callsign"`
	Repeater  apimodels.WSCallResponseRepeater `json:"repeater"`
	Alias     string                           `json:"alias"`
	Active    bool                             `json:"active"`
	Duration  int64                            `json:"duration_ms"`
}

// ListenWebsocket is the listen-only talkgroup monitor of DMRHub Radio. A
// visitor with no account tunes it to a talkgroup and gets the voice
// bursts of the group calls to it as DMRD packets in binary messages, and
// the calls in text messages. The session never sends anything into the
// network: it is not a repeater or a hotspot, so the hub does not route,
// track or record it, and it closes the session on any binary message.
type ListenWebsocket struct {
	current config.Source
	hub     *hub.Hub
	db      *gorm.DB
	pubsub  pubsub.PubSub

	mu        sync.Mutex
	count     int
	byAddress map[string]int
	sessions  map[websocket.Writer]*listenSession
}

type listenSession struct {
	w       websocket.Writer
	address string
	mu      sync.Mutex
	tg      uint
	stop    func()
}

// CreateListenWebsocket returns the listen-only monitor of the replica of
// dmrHub. It follows the visibility and the listen limits of current.
func CreateListenWebsocket(current config.Source, dmrHub *hub.Hub, db *gorm.DB, ps pubsub.PubSub) *ListenWebsocket {
	l := &ListenWebsocket{
		current:   current,
		hub:       dmrHub,
		db:        db,
		pubsub:    ps,
		byAddress: map[string]int{},
		sessions:  map[websocket.Writer]*listenSession{},
	}
	if dmrHub != nil {
		dmrHub.OnTalkgroupChange(l.talkgroupChanged)
	}
	return l
}

type listenAddressKey struct{}

// ListenAddress keeps the client address of the request, as the trusted
// proxies give it, for the per-address limit of the listen monitor.
func ListenAddress() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), listenAddressKey{}, c.ClientIP()))
		c.Next()
	}
}

func addressOf(r *http.Request) string {
	if a, ok := r.Context().Value(listenAddressKey{}).(string); ok && a != "" {
		return a
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func listenOpen(cfg *config.Config) bool {
	return cfg.Visibility.Mode() != config.VisibilityLogin && cfg.Listen.Listeners > 0
}

func (l *ListenWebsocket) OnConnect(ctx context.Context, r *http.Request, w websocket.Writer, _ sessions.Session) func() {
	cfg := l.current.Get()
	if l.hub == nil || l.db == nil || !listenOpen(cfg) {
		websocket.Refuse(w, ListenRefusedOff)
		return nil
	}
	s := &listenSession{w: w, address: addressOf(r)}
	if reason := l.admit(cfg, s); reason != "" {
		websocket.Refuse(w, reason)
		return nil
	}
	talkgroups, err := models.ListenTalkgroups(l.db)
	if err != nil {
		slog.Error("Failed to list the talkgroups of the listen monitor", "error", err)
		talkgroups = []models.ListenTalkgroup{}
	}
	writeJSON(w, ListenHello{Type: listenTypeHello, Network: cfg.NetworkName, Talkgroups: talkgroups})

	callsCtx, cancel := context.WithCancel(ctx)
	calls := l.pubsub.Subscribe("calls:public")
	var wg sync.WaitGroup
	wg.Go(func() { l.forwardCalls(callsCtx, s, calls) })
	return func() {
		cancel()
		closeSubscription(calls, "listen")
		wg.Wait()
		s.tune(0, nil)
		l.release(s)
	}
}

func (l *ListenWebsocket) admit(cfg *config.Config, s *listenSession) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.count >= cfg.Listen.Listeners {
		return ListenRefusedFull
	}
	if cfg.Listen.PerAddress > 0 && l.byAddress[s.address] >= cfg.Listen.PerAddress {
		return ListenRefusedAddress
	}
	l.count++
	l.byAddress[s.address]++
	l.sessions[s.w] = s
	return ""
}

func (l *ListenWebsocket) release(s *listenSession) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.sessions[s.w]; !ok {
		return
	}
	delete(l.sessions, s.w)
	l.count--
	if l.byAddress[s.address]--; l.byAddress[s.address] <= 0 {
		delete(l.byAddress, s.address)
	}
}

func (l *ListenWebsocket) session(w websocket.Writer) *listenSession {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sessions[w]
}

func (l *ListenWebsocket) OnMessage(_ context.Context, _ *http.Request, w websocket.Writer, _ sessions.Session, msg []byte, t int) {
	if t != int(coderWebsocket.MessageText) {
		websocket.Refuse(w, ListenRefusedTransmit)
		return
	}
	s := l.session(w)
	if s == nil {
		return
	}
	var tune ListenTune
	if err := json.Unmarshal(msg, &tune); err != nil || tune.Tune == nil {
		return
	}
	tg := *tune.Tune
	if tg == 0 {
		s.tune(0, nil)
		writeJSON(w, ListenTuned{Type: listenTypeTuned})
		return
	}
	if !listenOpen(l.current.Get()) {
		websocket.Refuse(w, ListenRefusedOff)
		return
	}
	ok, err := models.Listenable(l.db, tg)
	switch {
	case err != nil:
		slog.Error("Failed to look up a talkgroup for the listen monitor", "talkgroupID", tg, "error", err)
		writeJSON(w, ListenTuned{Type: listenTypeTuned, Talkgroup: s.current(), Error: listenErrLookup})
		return
	case !ok:
		writeJSON(w, ListenTuned{Type: listenTypeTuned, Talkgroup: s.current(), Error: listenErrNoTalkgroup})
		return
	}
	s.tune(tg, func() func() {
		return l.hub.Listen(tg, func(p models.Packet) { l.deliver(s, tg, p) })
	})
	writeJSON(w, ListenTuned{Type: listenTypeTuned, Talkgroup: tg})
}

func (s *listenSession) current() uint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tg
}

// tune starts the talkgroup tg with listen, or none when listen is nil,
// and then stops the talkgroup that s heard. It holds no lock while it
// starts or stops one, as the monitor calls deliver under its own.
func (s *listenSession) tune(tg uint, listen func() func()) {
	var next func()
	if listen == nil {
		tg = 0
	} else {
		next = listen()
	}
	s.mu.Lock()
	old := s.stop
	s.tg, s.stop = tg, next
	s.mu.Unlock()
	if old != nil {
		old()
	}
}

// deliver sends the burst p of the talkgroup tg to s as a DMRD packet. The
// repeater field is 0, as the burst comes from no repeater of the
// listener.
func (l *ListenWebsocket) deliver(s *listenSession, tg uint, p models.Packet) {
	if !listenOpen(l.current.Get()) {
		websocket.Refuse(s.w, ListenRefusedOff)
		return
	}
	if s.current() != tg {
		return
	}
	p.Repeater = 0
	s.w.WriteMessage(websocket.Message{Type: int(coderWebsocket.MessageBinary), Data: p.EncodeTo(nil)})
}

func (l *ListenWebsocket) forwardCalls(ctx context.Context, s *listenSession, sub pubsub.Subscription) {
	ch := sub.Channel()
	for {
		var msg []byte
		var ok bool
		select {
		case <-ctx.Done():
			return
		case msg, ok = <-ch:
		}
		if !ok {
			return
		}
		tg := s.current()
		if tg == 0 {
			continue
		}
		var call apimodels.WSCallResponse
		if err := json.Unmarshal(msg, &call); err != nil {
			continue
		}
		if !call.GroupCall || !call.IsToTalkgroup || call.ToTalkgroup.ID != tg || call.Kind != models.CallKindVoice {
			continue
		}
		switch l.current.Get().Visibility.Mode() {
		case config.VisibilityLogin:
			continue
		case config.VisibilityHideNames:
			call.HideNames()
		case config.VisibilityPublic:
		}
		source := call.SourceID
		if source == 0 {
			source = call.User.ID
		}
		writeJSON(s.w, ListenCall{
			Type:      listenTypeCall,
			Talkgroup: tg,
			Source:    source,
			Callsign:  call.User.Callsign,
			Repeater:  call.Repeater,
			Alias:     call.TalkerAlias,
			Active:    call.Active,
			Duration:  call.Duration.Milliseconds(),
		})
	}
}

// talkgroupChanged takes the sessions off the talkgroup id when its admins
// take it off the monitor or delete it.
func (l *ListenWebsocket) talkgroupChanged(id uint) {
	l.mu.Lock()
	var tuned []*listenSession
	for _, s := range l.sessions {
		if s.current() == id {
			tuned = append(tuned, s)
		}
	}
	l.mu.Unlock()
	if len(tuned) == 0 {
		return
	}
	ok, err := models.Listenable(l.db, id)
	if err != nil || ok {
		return
	}
	for _, s := range tuned {
		s.tune(0, nil)
		writeJSON(s.w, ListenTuned{Type: listenTypeTuned, Error: listenErrNoTalkgroup})
	}
}

func writeJSON(w websocket.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("Failed to write a listen message", "error", err)
		return
	}
	w.WriteMessage(websocket.Message{Type: int(coderWebsocket.MessageText), Data: b})
}
