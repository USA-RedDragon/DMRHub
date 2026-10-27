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

// Package modem drives an MMDVM modem board (MMDVM_HS hats, MMDVM repeater
// boards, DVMega) over a serial port with the MMDVM serial protocol, so that
// DMRHub is a hotspot or repeater with no MMDVMHost. The modem is a
// repeater of type models.RepeaterTypeModem to the hub: its RF calls go to
// the hub as DMRD packets of its repeater ID, and the hub routes calls to
// it as to any repeater. The base station logic is dmrgo bs.Repeater.
package modem

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"gorm.io/gorm"
)

const (
	versionTries  = 6
	claimRenew    = time.Minute
	pingWrite     = 30 * time.Second
	finishTimeout = 200 * time.Millisecond
	hubQueue      = 256
	packageIDLen  = 40
	slotsDuplex   = 3
	slotsSimplex  = 4
	softwareID    = "DMRHub"
)

var (
	errStopped     = errors.New("modem server stopped")
	errNoVersion   = errors.New("no version reply from the modem")
	errProtocol    = errors.New("modem protocol not supported")
	errNoDMR       = errors.New("modem firmware has no DMR")
	errNak         = errors.New("modem refused the command")
	errNoReply     = errors.New("no reply from the modem")
	errNoRepeater  = errors.New("repeater of the modem does not exist")
	errRepeaterID  = errors.New("repeater ID of the modem does not fit in 32 bits")
	errModemSilent = errors.New("no status from the modem for 2 s")
)

// Options are the parts of a Server that tests replace.
type Options struct {
	// Open opens the serial port. Nil means OpenSerial.
	Open Opener
	// OpenDelay is the wait after the port opens, for the boards that reset
	// when it does.
	OpenDelay time.Duration
	// Reply is how long the server waits for each reply in the open
	// sequence.
	Reply time.Duration
	// Reopen is the wait before the first open after the link went down.
	Reopen time.Duration
	// Retry is the wait between opens that fail.
	Retry time.Duration
}

// DefaultOptions are the options of MMDVMHost: 2 s after the port opens,
// 2 s before it opens again after a fault, then 5 s between tries.
func DefaultOptions() Options {
	return Options{Open: OpenSerial, OpenDelay: 2 * time.Second, Reply: time.Second, Reopen: 2 * time.Second, Retry: 5 * time.Second}
}

// Server drives one modem and is the server of the hub for the repeater
// type models.RepeaterTypeModem.
type Server struct {
	m       config.Modem
	id      uint32
	hub     *hub.Hub
	db      *gorm.DB
	version string
	opts    Options
	epoch   time.Time
	rssi    rssiMap

	ctx        context.Context //nolint:containedctx // the server outlives Start, as the other servers do
	handle     *hub.ServerHandle
	stopClaims func()
	fromHub    chan models.Packet
	linked     atomic.Bool
	jobs       servers.RepeaterQueue
	done       chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
}

// NewServer returns the server of the modem of cfg. It opens nothing until
// Start.
func NewServer(cfg *config.Config, h *hub.Hub, db *gorm.DB, version string) *Server {
	return NewServerWithOptions(cfg, h, db, version, DefaultOptions())
}

// NewServerWithOptions is NewServer with opts.
func NewServerWithOptions(cfg *config.Config, h *hub.Hub, db *gorm.DB, version string, opts Options) *Server {
	if opts.Open == nil {
		opts.Open = OpenSerial
	}
	m := cfg.DMR.Modem
	return &Server{m: m, id: uint32(min(m.RepeaterID, maxUint32)), hub: h, db: db, version: version, opts: opts, //nolint:gosec // clamped
		epoch: time.Now(), fromHub: make(chan models.Packet, hubQueue), done: make(chan struct{})}
}

// Addr returns the serial device of the modem.
func (s *Server) Addr() net.Addr {
	return Addr(s.m.Port)
}

// Linked reports whether the link to the modem is up.
func (s *Server) Linked() bool {
	return s.linked.Load()
}

// Start registers the server with the hub and starts to open the modem.
// It does not wait for the modem: the server tries to open it until Stop.
func (s *Server) Start(ctx context.Context) error {
	if uint(s.id) != s.m.RepeaterID {
		return errRepeaterID
	}
	s.ctx = context.WithoutCancel(ctx)
	s.loadRSSI()
	if s.hub != nil {
		s.handle = s.hub.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeModem, Role: hub.RoleRepeater})
		s.stopClaims = s.hub.OnClaimedElsewhere(func(id uint, _ time.Time) {
			if id == uint(s.id) && s.linked.Load() {
				slog.Error("Another replica claimed the repeater of the modem; turn the modem on in one replica only", "repeaterID", id)
			}
		})
		s.wg.Add(1)
		go s.forward()
	}
	s.wg.Add(1)
	go s.run()
	slog.Info("Modem server started", "port", s.m.Port, "repeaterID", s.id, "duplex", s.m.Duplex)
	return nil
}

// Stop turns the transmitter off, closes the port and unregisters the
// server. A modem stays on its host, so a graceful handoff stops it too.
func (s *Server) Stop(context.Context) error {
	s.stopOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
		if s.stopClaims != nil {
			s.stopClaims()
		}
		if s.hub != nil {
			s.hub.UnregisterServer(models.RepeaterTypeModem)
		}
		s.jobs.Wait()
	})
	return nil
}

// loadRSSI reads the RSSI mapping file of the modem. Without one, or when
// it does not load, the RSSI of modem calls is not known.
func (s *Server) loadRSSI() {
	path := s.m.RSSIMappingFile
	if path == "" {
		return
	}
	m, err := loadRSSIMap(path)
	if err != nil {
		slog.Warn("Could not load the RSSI mapping file of the modem; the RSSI of modem calls is not known", "file", path, "error", err)
		return
	}
	s.rssi = m
	slog.Info("Loaded the RSSI mapping of the modem", "file", path, "points", m.points())
}

func (s *Server) now() time.Duration {
	return time.Since(s.epoch)
}

// sleep waits d and reports whether the server still runs.
func (s *Server) sleep(d time.Duration) bool {
	if d <= 0 {
		select {
		case <-s.done:
			return false
		default:
			return true
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-s.done:
		return false
	case <-t.C:
		return true
	}
}

// forward takes the packets that the hub routes to the modem, and drops
// them while the link is down.
func (s *Server) forward() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case rp, ok := <-s.handle.Packets:
			if !ok {
				return
			}
			if rp.Barrier != nil {
				close(rp.Barrier)
				continue
			}
			if rp.RepeaterID != uint(s.id) || !s.linked.Load() {
				continue
			}
			select {
			case s.fromHub <- rp.Packet:
			default:
				slog.Warn("Modem queue full, dropping a packet from the hub", "repeaterID", s.id)
			}
		}
	}
}

func (s *Server) run() {
	defer s.wg.Done()
	var wait time.Duration
	for s.sleep(wait) {
		l, v, err := s.open()
		if err != nil {
			if !errors.Is(err, errStopped) {
				slog.Error("Could not open the modem", "port", s.m.Port, "error", err)
			}
			wait = s.opts.Retry
			continue
		}
		if err := s.serve(l, v); err != nil {
			slog.Error("Modem link down", "port", s.m.Port, "error", err)
		}
		wait = s.opts.Reopen
	}
}

// open opens the port and runs the open sequence of MMDVMHost: the
// version, the frequencies, the config and idle mode.
func (s *Server) open() (*link, mmdvm.Version, error) {
	p, err := s.opts.Open(s.m.Port, s.m.Baud)
	if err != nil {
		return nil, mmdvm.Version{}, err
	}
	l := newLink(p)
	v, err := s.handshake(l)
	if err != nil {
		l.close()
		return nil, mmdvm.Version{}, err
	}
	if err := s.claimRecord(v); err != nil {
		l.close()
		return nil, mmdvm.Version{}, err
	}
	slog.Info("Modem connected", "port", s.m.Port, "protocol", v.Protocol, "firmware", string(v.Description), "repeaterID", s.id)
	return l, v, nil
}

func (s *Server) handshake(l *link) (mmdvm.Version, error) {
	if !s.sleep(s.opts.OpenDelay) {
		return mmdvm.Version{}, errStopped
	}
	v, err := s.readVersion(l)
	if err != nil {
		return v, err
	}
	m := &s.m
	freq := setFreq(m)
	if err := s.command(l, mmdvm.CommandSetFreq, &freq); err != nil {
		return v, err
	}
	cfg, err := configFrame(m, v.Protocol)
	if err != nil {
		return v, err
	}
	if err := s.await(l, mmdvm.CommandSetConfig, cfg); err != nil {
		return v, err
	}
	idle, err := modeFrame(mmdvm.ModeIdle)
	if err != nil {
		return v, err
	}
	l.send(idle)
	return v, nil
}

func (s *Server) readVersion(l *link) (mmdvm.Version, error) {
	get, err := frame(mmdvm.CommandGetVersion, nil)
	if err != nil {
		return mmdvm.Version{}, err
	}
	for range versionTries {
		l.send(get)
		f, err := s.next(l, func(f mmdvm.Frame) bool { return f.Command == mmdvm.CommandGetVersion })
		if errors.Is(err, errNoReply) {
			continue
		}
		if err != nil {
			return mmdvm.Version{}, err
		}
		v, err := mmdvm.DecodeVersion(f.Payload)
		switch {
		case err != nil:
			return v, fmt.Errorf("%w: %w", errNoVersion, err)
		case v.Protocol != 1 && v.Protocol != 2:
			return v, fmt.Errorf("%w: %d", errProtocol, v.Protocol)
		case !v.Capabilities.Has(mmdvm.CapDMR):
			return v, errNoDMR
		}
		v.Description = append([]byte(nil), v.Description...)
		return v, nil
	}
	return mmdvm.Version{}, errNoVersion
}

func (s *Server) command(l *link, c mmdvm.Command, msg appender) error {
	f, err := frame(c, msg)
	if err != nil {
		return err
	}
	return s.await(l, c, f)
}

// await sends f and waits for the ACK or NAK of command c.
func (s *Server) await(l *link, c mmdvm.Command, f []byte) error {
	l.send(f)
	r, err := s.next(l, func(r mmdvm.Frame) bool {
		switch r.Command { //nolint:exhaustive // only an answer to c ends the wait
		case mmdvm.CommandAck:
			a, err := mmdvm.DecodeAck(r.Payload)
			return err == nil && a.Command == c
		case mmdvm.CommandNak:
			n, err := mmdvm.DecodeNak(r.Payload)
			return err == nil && n.Command == c
		}
		return false
	})
	if err != nil {
		return fmt.Errorf("%s: %w", c, err)
	}
	if r.Command == mmdvm.CommandNak {
		n, _ := mmdvm.DecodeNak(r.Payload)
		return fmt.Errorf("%s: %w: reason %d", c, errNak, n.Reason)
	}
	return nil
}

// next returns the next frame from l that want takes, within opts.Reply.
func (s *Server) next(l *link, want func(mmdvm.Frame) bool) (mmdvm.Frame, error) {
	t := time.NewTimer(s.opts.Reply)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return mmdvm.Frame{}, errStopped
		case <-l.failed:
			return mmdvm.Frame{}, l.cause()
		case <-t.C:
			return mmdvm.Frame{}, errNoReply
		case f := <-l.frames:
			if want(f) {
				return f, nil
			}
		}
	}
}

// claimRecord makes the repeater of the modem a modem repeater with the
// RF settings of the modem.
func (s *Server) claimRecord(v mmdvm.Version) error {
	if s.db == nil {
		return nil
	}
	var r models.Repeater
	err := s.db.First(&r, s.id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %d; create it first", errNoRepeater, s.id)
	}
	if err != nil {
		return fmt.Errorf("load the repeater of the modem: %w", err)
	}
	slots := uint(slotsSimplex)
	if s.m.Duplex {
		slots = slotsDuplex
	}
	pkg := string(v.Description)
	if len(pkg) > packageIDLen {
		pkg = pkg[:packageIDLen]
	}
	now := time.Now()
	freq := setFreq(&s.m)
	cols := map[string]any{"type": models.RepeaterTypeModem, "connected": now, "last_ping": now,
		"rx_frequency": uint(freq.RX), "tx_frequency": uint(freq.TX), "color_code": uint8(colorCode(&s.m)),
		"slots": slots, "software_id": softwareID, "package_id": pkg}
	if err := s.db.Model(&models.Repeater{}).Where("id = ?", s.id).UpdateColumns(cols).Error; err != nil {
		return fmt.Errorf("save the repeater of the modem: %w", err)
	}
	if r.Type != models.RepeaterTypeModem {
		slog.Warn("The repeater is now the modem of this server", "repeaterID", s.id, "was", r.Type)
		if s.hub != nil {
			s.hub.ReloadRepeater(s.ctx, uint(s.id))
		}
	}
	return nil
}

type linkSink struct {
	l *link
	s *Server
}

func (k linkSink) write(f []byte) { k.l.send(f) }

func (k linkSink) route(p models.Packet) {
	if k.s.hub != nil {
		k.s.hub.RoutePacket(k.s.ctx, p, models.RepeaterTypeModem)
	}
}

func (k linkSink) alias(a dmrd.TalkerAlias) {
	if k.s.hub != nil {
		k.s.hub.RecordTalkerAlias(a, models.RepeaterTypeModem, uint(k.s.id))
	}
}

func (k linkSink) position(p dmrd.Position) {
	if k.s.hub != nil {
		k.s.hub.RecordPosition(p, models.RepeaterTypeModem, uint(k.s.id))
	}
}

// serve runs the link l until it fails or the server stops.
func (s *Server) serve(l *link, v mmdvm.Version) error {
	start := s.now()
	r, err := newRadio(&s.m, s.id, v.Protocol, linkSink{l: l, s: s}, start, s.rssi)
	if err != nil {
		l.close()
		return err
	}
	s.linked.Store(true)
	s.activate()
	defer func() {
		s.linked.Store(false)
		s.deactivate()
	}()
	renew, ping := start+claimRenew, start+pingWrite
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-s.done:
			r.stop()
			l.finish(finishTimeout)
			return nil
		case <-l.failed:
			err := l.cause()
			l.close()
			return err
		case f := <-l.frames:
			r.frame(s.now(), f)
		case p := <-s.fromHub:
			r.packet(s.now(), p)
		case <-timer.C:
		}
		now := s.now()
		r.tick(now)
		if r.silent(now) {
			l.close()
			return errModemSilent
		}
		if now >= renew {
			renew = now + claimRenew
			s.renew()
		}
		if now >= ping {
			ping = now + pingWrite
			s.writePing()
		}
		timer.Reset(max(r.deadline()-s.now(), 0))
	}
}

func (s *Server) activate() {
	if s.hub == nil {
		return
	}
	id, since := uint(s.id), time.Now()
	s.jobs.Do(id, func() { s.hub.ActivateRepeaterSince(s.ctx, id, since) })
}

func (s *Server) deactivate() {
	if s.hub == nil {
		return
	}
	id := uint(s.id)
	s.jobs.Do(id, func() { s.hub.DeactivateRepeater(s.ctx, id) })
}

func (s *Server) renew() {
	if s.hub == nil {
		return
	}
	id := uint(s.id)
	s.jobs.Do(id, func() { s.hub.RenewOwnership(s.ctx, id) })
}

func (s *Server) writePing() {
	if s.db == nil {
		return
	}
	id := uint(s.id)
	s.jobs.Do(id, func() {
		if err := s.db.Model(&models.Repeater{}).Where("id = ?", id).UpdateColumn("last_ping", time.Now()).Error; err != nil {
			slog.Error("Could not save the last ping of the modem", "repeaterID", id, "error", err)
		}
	})
}
