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
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
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

type sentPosition struct {
	at       time.Time
	lat, lon float64
}

// Gateway sends the positions of the radios whose owners turn APRS-IS on.
// Of the replicas, only the one that holds the lease in the KV store logs
// in and sends; the others drop the positions.
type Gateway struct {
	db      *gorm.DB
	cfg     *config.Config
	kv      kv.KV
	pubsub  pubsub.PubSub
	version string
	id      string
	every   time.Duration
	now     func() time.Time

	mu     sync.Mutex
	client *aprs.Client
	leader bool
	sent   map[uint]sentPosition

	cancel context.CancelFunc
	done   chan struct{}
}

// NewGateway returns a Gateway that reads the APRS-IS login from cfg at
// each lease check, so a change in the Setup page takes effect within
// seconds.
func NewGateway(db *gorm.DB, cfg *config.Config, kvStore kv.KV, ps pubsub.PubSub, version string) *Gateway {
	var id [8]byte
	_, _ = rand.Read(id[:])
	return &Gateway{
		db: db, cfg: cfg, kv: kvStore, pubsub: ps, version: version, id: hex.EncodeToString(id[:]),
		every: leaseEvery, now: time.Now, sent: map[uint]sentPosition{},
	}
}

// Start follows the stored positions until Stop.
func (g *Gateway) Start(ctx context.Context) {
	ctx, g.cancel = context.WithCancel(ctx)
	g.done = make(chan struct{})
	sub := g.pubsub.Subscribe(AllTopic)
	go func() {
		defer close(g.done)
		defer func() { _ = sub.Close() }()
		ticker := time.NewTicker(g.every)
		defer ticker.Stop()
		g.refresh(ctx)
		for {
			select {
			case <-ctx.Done():
				g.release(context.WithoutCancel(ctx))
				return
			case <-ticker.C:
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
	is := g.cfg.DMR.Positions.APRSIS
	c := aprs.Config{Login: is.Callsign, Passcode: is.Passcode, Server: is.ServerAddress()}
	return c, c.Login != ""
}

func (g *Gateway) refresh(ctx context.Context) {
	want, on := g.login()
	if !on {
		g.release(ctx)
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
		g.client.Start(ctx)
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
	if g.leader {
		if v, err := g.kv.Get(ctx, leaseKey); err == nil && string(v) == g.id {
			_ = g.kv.Delete(ctx, leaseKey)
		}
	}
	g.leader = false
}

func (g *Gateway) handle(p models.Position) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.leader || g.client == nil {
		return
	}
	line, reason := g.line(&p)
	if line == "" {
		slog.Debug("APRS-IS: not sending a position", "radio", p.RadioID, "reason", reason)
		return
	}
	if err := g.client.Send(line); err != nil {
		slog.Warn("APRS-IS: cannot send a position", "radio", p.RadioID, "error", err)
		return
	}
	g.sent[p.RadioID] = sentPosition{at: p.ReceivedAt, lat: p.Latitude, lon: p.Longitude}
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
	now := g.now()
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
