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

package positions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"gorm.io/gorm"
)

const (
	leaseKey   = "dmrhub:aprs:lease"
	leaseTTL   = 30 * time.Second
	leaseEvery = 10 * time.Second
	// APRSInterval is the shortest time between two packets of one radio.
	APRSInterval = time.Minute
	// maxAge is how old a position may be when the gateway gets it.
	maxAge = 2 * time.Minute
	// maxFixAge is how much older than its arrival the fix of a position
	// may be.
	maxFixAge = 10 * time.Minute
	// defaultAccuracy is how far a radio must move when its position has no
	// accuracy.
	defaultAccuracy = 10.0
	earthRadiusM    = 6371000.0
	defaultSymbol   = "car"
)

type observer struct {
	refreshed func(connected bool)
	handled   func(p models.Position, sent bool)
	login     func(verified bool)
	message   func(m aprs.Message, sent bool)
}

// MessageTopic is the pubsub topic of the APRS messages that plugins send,
// as JSON of aprs.Message. The replica that holds the APRS-IS login sends
// them.
const MessageTopic = "aprs:messages"

// PublishMessage hands m to the replica that holds the APRS-IS login,
// which sends it. Nothing tells whether it did: a sender that wants to
// know asks for an ack in m.
func PublishMessage(ps pubsub.PubSub, m aprs.Message) error {
	if err := m.Check(); err != nil {
		return err //nolint:wrapcheck // ErrMessage of the aprs package
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("could not encode an APRS message: %w", err)
	}
	if err := ps.Publish(MessageTopic, data); err != nil {
		return fmt.Errorf("could not publish an APRS message: %w", err)
	}
	return nil
}

type sentPosition struct {
	at       time.Time
	lat, lon float64
}

// Gateway sends the positions of the radios whose owners turn APRS-IS on.
// Of the replicas, only the one that holds the lease in the KV store logs
// in and sends; the others drop the positions.
type Gateway struct {
	db      *gorm.DB
	cfg     config.Source
	kv      kv.KV
	pubsub  pubsub.PubSub
	version string
	id      string
	every   time.Duration
	clock   clock.Clock

	mu     sync.Mutex
	client *aprs.Client
	leader bool
	sent   map[uint]sentPosition
	obs    observer

	cancel context.CancelFunc
	done   chan struct{}
}

// NewGateway returns a Gateway that reads the APRS-IS login from cfg at
// each lease check, so a change in the Setup page takes effect within
// seconds.
func NewGateway(db *gorm.DB, cfg config.Source, kvStore kv.KV, ps pubsub.PubSub, version string) *Gateway {
	var id [8]byte
	_, _ = rand.Read(id[:])
	return &Gateway{
		db: db, cfg: cfg, kv: kvStore, pubsub: ps, version: version, id: hex.EncodeToString(id[:]),
		every: leaseEvery, clock: clock.Real(), sent: map[uint]sentPosition{},
	}
}

// Start follows the stored positions until Stop.
func (g *Gateway) Start(ctx context.Context) {
	ctx, g.cancel = context.WithCancel(ctx)
	g.done = make(chan struct{})
	sub := g.pubsub.Subscribe(AllTopic)
	messages := g.pubsub.Subscribe(MessageTopic)
	go func() {
		defer close(g.done)
		defer func() { _ = sub.Close() }()
		defer func() { _ = messages.Close() }()
		ticker := g.clock.NewTicker(g.every)
		defer ticker.Stop()
		g.refresh(ctx)
		for {
			select {
			case <-ctx.Done():
				g.release(context.WithoutCancel(ctx))
				return
			case <-ticker.C():
				g.refresh(ctx)
			case b, ok := <-sub.Channel():
				if !ok {
					return
				}
				var p models.Position
				if err := json.Unmarshal(b, &p); err != nil {
					slog.Error("APRS-IS: cannot decode a position", "error", err)
					continue
				}
				g.handle(p)
			case b, ok := <-messages.Channel():
				if !ok {
					return
				}
				var m aprs.Message
				if err := json.Unmarshal(b, &m); err != nil {
					slog.Error("APRS-IS: cannot decode a message", "error", err)
					continue
				}
				g.message(m)
			}
		}
	}()
}

// Stop logs out and gives up the lease.
func (g *Gateway) Stop() {
	if g.cancel == nil {
		return
	}
	g.cancel()
	<-g.done
}

func (g *Gateway) login() (aprs.Config, bool) {
	is := g.cfg.Get().DMR.Positions.APRSIS
	c := aprs.Config{Login: is.Callsign, Passcode: aprs.Passcode(is.Callsign), Server: is.ServerAddress()}
	return c, c.Login != ""
}

func (g *Gateway) refresh(ctx context.Context) {
	want, on := g.login()
	if !on {
		g.release(ctx)
		if g.obs.refreshed != nil {
			g.obs.refreshed(false)
		}
		return
	}
	leader := g.holdLease(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.leader = leader
	if g.client != nil && (!leader || g.client.Config() != want) {
		g.client.Stop()
		g.client = nil
	}
	if leader && g.client == nil {
		g.client = aprs.NewClient(want, g.version)
		if g.obs.login != nil {
			g.client.OnLogin(g.obs.login)
		}
		g.client.Start(ctx)
	}
	if g.obs.refreshed != nil {
		g.obs.refreshed(g.client != nil)
	}
}

func (g *Gateway) holdLease(ctx context.Context) bool {
	if v, err := g.kv.Get(ctx, leaseKey); err == nil && string(v) == g.id {
		if err := g.kv.Expire(ctx, leaseKey, leaseTTL); err != nil {
			slog.Error("APRS-IS: cannot keep the lease", "error", err)
			return false
		}
		return true
	}
	ok, err := g.kv.SetNX(ctx, leaseKey, g.id, leaseTTL)
	if err != nil {
		slog.Error("APRS-IS: cannot take the lease", "error", err)
		return false
	}
	return ok
}

func (g *Gateway) release(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.client != nil {
		g.client.Stop()
		g.client = nil
	}
	if _, err := g.kv.DeleteIfEqual(ctx, leaseKey, []byte(g.id)); err != nil {
		slog.Error("APRS-IS: cannot give up the lease", "error", err)
	}
	g.leader = false
}

func (g *Gateway) handle(p models.Position) {
	g.mu.Lock()
	defer g.mu.Unlock()
	sent := g.send(&p)
	if g.obs.handled != nil {
		g.obs.handled(p, sent)
	}
}

func (g *Gateway) message(m aprs.Message) {
	g.mu.Lock()
	defer g.mu.Unlock()
	sent := g.sendMessage(&m)
	if g.obs.message != nil {
		g.obs.message(m, sent)
	}
}

func (g *Gateway) sendMessage(m *aprs.Message) bool {
	if !g.leader || g.client == nil {
		return false
	}
	login, _ := g.login()
	line, err := login.MessageLine(m)
	if err != nil {
		slog.Warn("APRS-IS: not sending a message", "source", m.Source, "error", err)
		return false
	}
	if err := g.client.Send(line); err != nil {
		slog.Warn("APRS-IS: cannot send a message", "source", m.Source, "error", err)
		return false
	}
	return true
}

func (g *Gateway) send(p *models.Position) bool {
	if !g.leader || g.client == nil {
		return false
	}
	line, reason := g.line(p)
	if line == "" {
		slog.Debug("APRS-IS: not sending a position", "radio", p.RadioID, "reason", reason)
		return false
	}
	if err := g.client.Send(line); err != nil {
		slog.Warn("APRS-IS: cannot send a position", "radio", p.RadioID, "error", err)
		return false
	}
	g.sent[p.RadioID] = sentPosition{at: p.ReceivedAt, lat: p.Latitude, lon: p.Longitude}
	return true
}

// line returns the packet of p, or why p is not sent.
func (g *Gateway) line(p *models.Position) (string, string) {
	s, ok, err := models.FindPositionSetting(g.db, p.RadioID)
	if err != nil || !ok || !s.Tracking || !s.APRS {
		return "", "APRS-IS is off for the radio"
	}
	var owner models.User
	if err := g.db.Where("id = ?", s.UserID).Limit(1).Find(&owner).Error; err != nil || owner.ID == 0 {
		return "", "no owner"
	}
	if !owner.Approved || owner.Suspended || !aprs.Licensed(owner.Callsign) {
		return "", "the owner has no licensed callsign"
	}
	now := g.clock.Now()
	if now.Sub(p.ReceivedAt) > maxAge || (p.FixTime != nil && p.ReceivedAt.Sub(*p.FixTime) > maxFixAge) {
		return "", "stale"
	}
	if last, ok := g.sent[p.RadioID]; ok {
		if p.ReceivedAt.Sub(last.at) < APRSInterval {
			return "", "sent less than a minute ago"
		}
		accuracy := defaultAccuracy
		if p.AccuracyM != nil && *p.AccuracyM > 0 {
			accuracy = *p.AccuracyM
		}
		if distance(last.lat, last.lon, p.Latitude, p.Longitude) <= accuracy {
			return "", "not moved"
		}
	}
	var rows []models.Repeater
	if err := g.db.Where("id = ?", p.RepeaterID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return "", "unknown repeater"
	}
	name := s.APRSSymbol
	if name == "" {
		if name, ok = aprs.ServiceSymbol(p.ServiceID); !ok {
			name = defaultSymbol
		}
	}
	sym, ok := aprs.LookupSymbol(name)
	if !ok {
		return "", "unknown symbol"
	}
	info, err := aprs.Report{
		Latitude: p.Latitude, Longitude: p.Longitude, Symbol: sym,
		HeadingDeg: p.HeadingDeg, SpeedMPS: p.SpeedMPS, AltitudeM: p.AltitudeM, Comment: s.APRSComment,
	}.Info()
	if err != nil {
		return "", err.Error()
	}
	source := owner.Callsign
	if s.APRSSSID != "" {
		source += "-" + s.APRSSSID
	}
	login, _ := g.login()
	line, err := login.Line(source, rows[0].Callsign, info)
	if err != nil {
		return "", err.Error()
	}
	return line, ""
}

func distance(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(a))
}
