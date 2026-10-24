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

package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// sendQueue is how many messages wait for a plugin that reads slowly before
// its bursts are dropped.
const sendQueue = 256

// heartbeatEvery is how often a stream records that its plugin is still
// there.
const heartbeatEvery = 15 * time.Second

// streamError ends a plugin's stream with a gRPC code.
type streamError struct {
	code codes.Code
	msg  string
}

func (e *streamError) Error() string {
	return e.msg
}

// GRPCStatus gives gRPC the code of e.
func (e *streamError) GRPCStatus() *status.Status {
	return status.New(e.code, e.msg)
}

// The errors that end a plugin's stream.
var (
	errTryLater     = &streamError{codes.Unavailable, "try again later"}
	errUnknownToken = &streamError{codes.Unauthenticated, "unknown token"}
	errOff          = &streamError{codes.PermissionDenied, "this plugin is off"}
	errDeleted      = &streamError{codes.PermissionDenied, "this plugin was deleted"}
	errNewToken     = &streamError{codes.Unauthenticated, "the token of this plugin was replaced"}
	errReplaced     = &streamError{codes.Aborted, "a newer connection of this plugin took over"}
)

// Router routes the bursts that plugins send, like those of a repeater.
type Router interface {
	RoutePacket(ctx context.Context, p models.Packet, sourceName string)
}

// Service serves the plugin protocol.
type Service struct {
	pluginv1.UnimplementedPluginServiceServer

	db        *gorm.DB
	pubsub    pubsub.PubSub
	registry  *Registry
	router    Router
	notify    func(models.Position)
	now       func() time.Time
	heartbeat time.Duration
}

// NewService returns the plugin service. Bursts that plugins send go to
// router; registry tells who owns each ID; notify gets each position that
// a plugin sent and the hub stored.
func NewService(db *gorm.DB, ps pubsub.PubSub, registry *Registry, router Router, notify func(models.Position)) *Service {
	return &Service{db: db, pubsub: ps, registry: registry, router: router, notify: notify, now: time.Now, heartbeat: heartbeatEvery}
}

func tokenOf(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, v := range md.Get("authorization") {
		if scheme, token, ok := strings.Cut(v, " "); ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token)
		}
	}
	return ""
}

func (s *Service) authenticate(ctx context.Context) (models.Plugin, error) {
	p, ok, err := models.PluginByToken(s.db, tokenOf(ctx))
	if err != nil {
		slog.Error("Plugins: cannot check a token", "error", err)
		return models.Plugin{}, errTryLater
	}
	if !ok {
		return models.Plugin{}, errUnknownToken
	}
	if !p.Enabled {
		return models.Plugin{}, errOff
	}
	return p, nil
}

func rangesOf(p *models.Plugin) []*pluginv1.IDRange {
	out := make([]*pluginv1.IDRange, len(p.Ranges))
	for i, r := range p.Ranges {
		out[i] = &pluginv1.IDRange{First: uint32(r.First), Last: uint32(r.Last)} //nolint:gosec // DMR IDs are 24 bits
	}
	return out
}

func sameRanges(a, b []models.PluginRange) bool {
	return slices.EqualFunc(a, b, func(x, y models.PluginRange) bool { return x.First == y.First && x.Last == y.Last })
}

// Connect serves the stream of one plugin. A newer stream of the same plugin,
// on any replica, ends this one.
func (s *Service) Connect(stream pluginv1.PluginService_ConnectServer) error {
	ctx := stream.Context()
	p, err := s.authenticate(ctx)
	if err != nil {
		return err
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	session := hex.EncodeToString(id[:])

	kick := s.pubsub.Subscribe(kickTopic(p.ID))
	defer func() { _ = kick.Close() }()
	in := s.pubsub.Subscribe(InTopic(p.ID))
	defer func() { _ = in.Close() }()
	changed := s.pubsub.Subscribe(ChangedTopic)
	defer func() { _ = changed.Close() }()
	if err := s.pubsub.Publish(kickTopic(p.ID), []byte(session)); err != nil {
		return errTryLater
	}
	address := ""
	if pr, ok := peer.FromContext(ctx); ok && pr.Addr != nil {
		address = pr.Addr.String()
	}
	if err := models.NotePluginConnected(s.db, p.ID, session, address, s.now()); err != nil {
		slog.Warn("Plugins: cannot note a connection", "plugin", p.Name, "error", err)
	}
	slog.Info("Plugins: connected", "plugin", p.Name, "address", address)
	defer func() {
		if err := models.NotePluginDisconnected(s.db, p.ID, session, s.now()); err != nil {
			slog.Warn("Plugins: cannot note a disconnection", "plugin", p.Name, "error", err)
		}
		slog.Info("Plugins: disconnected", "plugin", p.Name)
	}()

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	out := make(chan *pluginv1.ConnectResponse, sendQueue)
	out <- &pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Welcome{Welcome: &pluginv1.Welcome{Name: p.Name, Ranges: rangesOf(&p)}}}
	send := func(m *pluginv1.ConnectResponse) {
		select {
		case out <- m:
		default:
			slog.Warn("Plugins: dropping a message to a plugin that reads slowly", "plugin", p.Name)
		}
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-out:
				if err := stream.Send(m); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()

	ranges := make(chan []models.PluginRange, 1)
	ranges <- p.Ranges
	w := &watcher{
		s: s, cancel: cancel, p: &p, session: session, kick: kick, in: in, changed: changed, send: send, ranges: ranges,
		subscribe: make(chan bool, 1), positions: make(chan *pluginv1.Position, sendQueue), heardOn: map[uint32]uint{},
	}
	go w.watch(ctx)

	go s.receive(ctx, cancel, stream, &p, send, ranges, w)
	<-ctx.Done()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, errPluginDone) && !errors.Is(cause, context.Canceled) {
		return cause //nolint:wrapcheck // a gRPC status of this package, or the error of a failed send
	}
	return nil
}

// errPluginDone ends a stream that the plugin closed.
var errPluginDone = errors.New("the plugin closed its stream")

// receive routes the bursts that the plugin sends, and refuses those it may
// not send. It hands its subscriptions and positions to w.
func (s *Service) receive(ctx context.Context, cancel context.CancelCauseFunc, stream pluginv1.PluginService_ConnectServer,
	p *models.Plugin, send func(*pluginv1.ConnectResponse), ranges chan []models.PluginRange, w *watcher) {
	current := p.Ranges
	for {
		msg, err := stream.Recv()
		if err != nil {
			cancel(errPluginDone)
			return
		}
		select {
		case current = <-ranges:
		default:
		}
		switch m := msg.GetMessage().(type) {
		case *pluginv1.ConnectRequest_Subscribe:
			select {
			case <-w.subscribe:
			default:
			}
			w.subscribe <- m.Subscribe.GetTracking()
			continue
		case *pluginv1.ConnectRequest_Position:
			select {
			case w.positions <- m.Position:
			default:
				slog.Warn("Plugins: dropping a position of a plugin that sends too many", "plugin", p.Name)
			}
			continue
		}
		b := msg.GetBurst()
		if b == nil {
			continue
		}
		pkt, reason := FromBurst(b, p.ID, current, s.registry)
		if reason != pluginv1.RefusalReason_REFUSAL_REASON_UNSPECIFIED {
			send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Refusal{Refusal: &pluginv1.Refusal{StreamId: b.GetStreamId(), Reason: reason}}})
			continue
		}
		s.router.RoutePacket(context.WithoutCancel(ctx), pkt, SourceName(p.ID))
	}
}

// watcher follows one plugin's stream.
type watcher struct {
	s         *Service
	cancel    context.CancelCauseFunc
	p         *models.Plugin
	session   string
	kick      pubsub.Subscription
	in        pubsub.Subscription
	changed   pubsub.Subscription
	send      func(*pluginv1.ConnectResponse)
	ranges    chan []models.PluginRange
	subscribe chan bool
	positions chan *pluginv1.Position

	tracking pubsub.Subscription
	heard    pubsub.Subscription
	// heardOn is the repeater where each radio with tracking on was heard
	// last, when the plugin subscribed to tracking.
	heardOn map[uint32]uint
}

// watch forwards the bursts to the plugin, follows changes to it, and ends
// the stream when a newer stream of it starts or it is turned off.
func (w *watcher) watch(ctx context.Context) {
	defer w.unsubscribe()
	current := w.p.Ranges
	heartbeat := time.NewTicker(w.s.heartbeat)
	defer heartbeat.Stop()
	for {
		var tracking, heard <-chan []byte
		if w.tracking != nil {
			tracking, heard = w.tracking.Channel(), w.heard.Channel()
		}
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := models.NotePluginSeen(w.s.db, w.p.ID, w.session, w.s.now()); err != nil {
				slog.Warn("Plugins: cannot note a heartbeat", "plugin", w.p.Name, "error", err)
			}
		case b, ok := <-w.kick.Channel():
			if !ok {
				return
			}
			if string(b) != w.session {
				w.cancel(errReplaced)
				return
			}
		case b, ok := <-w.in.Channel():
			if !ok {
				return
			}
			var pkt models.Packet
			if _, err := pkt.UnmarshalMsg(b); err != nil {
				slog.Error("Plugins: cannot decode a burst", "error", err)
				continue
			}
			w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Burst{Burst: ToBurst(&pkt)}})
		case on := <-w.subscribe:
			w.setTracking(on)
		case pos := <-w.positions:
			w.store(pos, current)
		case b, ok := <-tracking:
			if !ok {
				return
			}
			w.trackingChanged(b)
		case b, ok := <-heard:
			if !ok {
				return
			}
			w.radioHeard(b)
		case _, ok := <-w.changed.Channel():
			if !ok {
				return
			}
			now, err := models.FindPlugin(w.s.db, w.p.ID)
			switch {
			case errors.Is(err, models.ErrPluginNotFound):
				w.cancel(errDeleted)
				return
			case err != nil:
				slog.Error("Plugins: cannot read a plugin", "plugin", w.p.Name, "error", err)
				continue
			case !now.Enabled:
				w.cancel(errOff)
				return
			case !slices.Equal(now.TokenHash, w.p.TokenHash):
				w.cancel(errNewToken)
				return
			}
			if !sameRanges(now.Ranges, current) {
				current = now.Ranges
				select {
				case <-w.ranges:
				default:
				}
				w.ranges <- current
				w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Ranges{Ranges: &pluginv1.Ranges{Ranges: rangesOf(&now)}}})
			}
		}
	}
}

// setTracking subscribes the plugin to the radios with tracking on, and
// sends them all, or ends that subscription.
func (w *watcher) setTracking(on bool) {
	if !on {
		w.unsubscribe()
		return
	}
	if w.tracking == nil {
		w.tracking = w.s.pubsub.Subscribe(TrackingTopic)
		w.heard = w.s.pubsub.Subscribe(HeardTopic)
	}
	radios, err := tracked(w.s.db)
	if err != nil {
		slog.Error("Plugins: cannot list the tracked radios", "plugin", w.p.Name, "error", err)
		return
	}
	known := make(map[uint32]uint, len(radios))
	for _, r := range radios {
		known[r.GetRadio()] = w.heardOn[r.GetRadio()]
	}
	w.heardOn = known
	w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Tracked{Tracked: &pluginv1.Tracked{Radios: radios}}})
}

func (w *watcher) unsubscribe() {
	if w.tracking == nil {
		return
	}
	_ = w.tracking.Close()
	_ = w.heard.Close()
	w.tracking, w.heard = nil, nil
	w.heardOn = map[uint32]uint{}
}

// trackingChanged sends the tracking of the radio named in b.
func (w *watcher) trackingChanged(b []byte) {
	radio, err := strconv.ParseUint(string(b), 10, 32)
	if err != nil {
		return
	}
	s, _, err := models.FindPositionSetting(w.s.db, uint(radio))
	if err != nil {
		slog.Error("Plugins: cannot read a position setting", "radio", radio, "error", err)
		return
	}
	if s.Tracking {
		w.heardOn[uint32(radio)] = w.heardOn[uint32(radio)]
	} else {
		delete(w.heardOn, uint32(radio))
	}
	w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Tracking{Tracking: trackingOf(&s)}})
}

// radioHeard tells the plugin where a radio with tracking on was heard.
func (w *watcher) radioHeard(b []byte) {
	h, ok := decodeHeard(b)
	if !ok {
		return
	}
	if _, tracked := w.heardOn[h.radio]; !tracked {
		return
	}
	w.heardOn[h.radio] = uint(h.repeater)
	w.send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Heard{Heard: &pluginv1.Heard{Radio: h.radio, Slot: uint32(h.slot), ColorCode: uint32(h.cc)}}})
}

// store keeps a position that the plugin sent, when the radio has tracking
// on and sent it to one of the plugin's IDs.
func (w *watcher) store(p *pluginv1.Position, ranges []models.PluginRange) {
	pos, ok := positionOf(p, ranges, w.heardOn[p.GetRadio()], w.s.now())
	if !ok {
		return
	}
	stored, err := models.StorePosition(w.s.db, pos)
	if err != nil {
		slog.Error("Plugins: cannot store a position", "plugin", w.p.Name, "radio", pos.RadioID, "error", err)
		return
	}
	if stored && w.s.notify != nil {
		w.s.notify(pos)
	}
}
