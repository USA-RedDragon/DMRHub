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
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"gorm.io/gorm"
)

const (
	// stopWait is how long Stop lets the streams end before it cuts them.
	stopWait = 5 * time.Second
	// pingEvery is how often the hub checks that a quiet plugin is still
	// there.
	pingEvery = 30 * time.Second
	// minPluginPing is the shortest time between two pings of a plugin.
	minPluginPing = 10 * time.Second
	// maxMessage is the largest message a plugin may send.
	maxMessage = 64 << 10
)

// Server is the plugin listener.
type Server struct {
	grpc     *grpc.Server
	listener net.Listener
	registry *Registry
	service  *Service
	done     chan struct{}
	stopOnce sync.Once
}

// Start listens for plugins as cfg says, and gives the hub the registry of
// their IDs through setPlugins. notify gets each position that a plugin
// sent and the hub stored. It returns nil when the listener is off.
func Start(ctx context.Context, cfg *config.Config, db *gorm.DB, ps pubsub.PubSub, router Router,
	setPlugins func(*Registry), notify func(models.Position)) (*Server, error) {
	c := cfg.DMR.Plugins
	if !c.Enabled {
		return nil, nil //nolint:nilnil // no listener when it is off
	}
	certs, err := NewCertSource(cfg.GetDerivedSecret(), c.CertFile, c.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("plugin certificate: %w", err)
	}
	registry, err := NewRegistry(db, ps)
	if err != nil {
		return nil, err
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", net.JoinHostPort(trimBrackets(c.Bind), strconv.Itoa(c.Port)))
	if err != nil {
		return nil, fmt.Errorf("could not listen for plugins: %w", err)
	}
	s := &Server{
		grpc: grpc.NewServer(
			grpc.Creds(credentials.NewTLS(&tls.Config{GetCertificate: certs.GetCertificate, MinVersion: tls.VersionTLS13})),
			grpc.KeepaliveParams(keepalive.ServerParameters{Time: pingEvery, Timeout: pingEvery}),
			grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: minPluginPing, PermitWithoutStream: true}),
			grpc.MaxRecvMsgSize(maxMessage),
		),
		listener: lis,
		registry: registry,
		service:  NewService(db, ps, registry, router, notify),
		done:     make(chan struct{}),
	}
	pluginv1.RegisterPluginServiceServer(s.grpc, s.service)
	registry.Follow(s.done)
	setPlugins(registry)
	go func() {
		if err := s.grpc.Serve(lis); err != nil {
			slog.Error("Plugins: the listener stopped", "error", err)
		}
	}()
	slog.Info("Plugins: listening", "address", lis.Addr().String())
	return s, nil
}

// Registry returns the registry of the plugins' IDs.
func (s *Server) Registry() *Registry {
	return s.registry
}

// Addr returns the address the listener listens on.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Drain moves the plugins with a stream on this replica to the other
// replicas, as Service.Drain does, until ctx ends. Call it only when other
// replicas run. It returns how many streams are left.
func (s *Server) Drain(ctx context.Context) int {
	if s == nil {
		return 0
	}
	slog.Info("Plugins: draining")
	left := s.service.Drain(ctx)
	slog.Info("Plugins: drain finished", "streamsLeft", left)
	return left
}

// Stop ends the streams of the plugins and closes the listener.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.done)
		stopped := make(chan struct{})
		go func() {
			s.grpc.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(stopWait):
			s.grpc.Stop()
		}
	})
}

func trimBrackets(host string) string {
	if len(host) > 1 && host[0] == '[' && host[len(host)-1] == ']' {
		return host[1 : len(host)-1]
	}
	return host
}
