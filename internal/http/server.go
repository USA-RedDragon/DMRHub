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

package http

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	ratelimit "github.com/JGLTechnologies/gin-rate-limit"
	configPkg "github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/netscheduler"
	"github.com/USA-RedDragon/DMRHub/internal/http/api"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/middleware"
	gormRateLimit "github.com/USA-RedDragon/DMRHub/internal/http/ratelimit"
	"github.com/USA-RedDragon/DMRHub/internal/http/setupwizard"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/sessions"
	gormSessions "github.com/gin-contrib/sessions/gorm"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/gorm"
)

var (
	ErrReadDir = errors.New("error reading directory")
)

type Server struct {
	*http.Server
	shutdownChannel chan bool
	stopOnce        sync.Once
}

const defTimeout = 10 * time.Second
const rateLimitRate = time.Second
const rateLimitLimit = 50

func MakeServer(ctx context.Context, config *configPkg.Config, dmrHub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, ready *atomic.Bool, ns *netscheduler.NetScheduler, version, commit string) Server {
	if config.LogLevel == configPkg.LogLevelDebug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := CreateRouter(ctx, config, dmrHub, db, pubsub, ready, ns, version, commit)

	slog.Info("HTTP Server listening", "bind", config.HTTP.Bind, "port", config.HTTP.Port)
	s := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.HTTP.Bind, config.HTTP.Port),
		Handler:      r,
		ReadTimeout:  defTimeout,
		WriteTimeout: defTimeout,
	}
	s.SetKeepAlivesEnabled(false)

	return Server{
		Server:          s,
		shutdownChannel: make(chan bool),
	}
}

func MakeSetupWizardServer(config *configPkg.Config, token string, configCompleteChan chan any, version, commit string) Server {
	if config.LogLevel == configPkg.LogLevelDebug {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := CreateSetupWizardRouter(config, token, configCompleteChan, version, commit)

	slog.Info("HTTP Server listening", "bind", "[::]", "port", "3005")
	s := &http.Server{
		Addr:         "[::]:3005",
		Handler:      r,
		ReadTimeout:  defTimeout,
		WriteTimeout: defTimeout,
	}
	s.SetKeepAlivesEnabled(false)

	return Server{
		Server:          s,
		shutdownChannel: make(chan bool),
	}
}

// FS is the embedded frontend files
//
//go:generate sh -c "cd ./frontend && npm ci && npm run build"
//go:embed frontend/dist/*
var FS embed.FS

func addMiddleware(_ context.Context, config *configPkg.Config, dmrHub *hub.Hub, r *gin.Engine, db *gorm.DB, pubsub pubsub.PubSub, ready *atomic.Bool, ns *netscheduler.NetScheduler, version, commit string) {
	// Tracing
	if config.Metrics.OTLPEndpoint != "" {
		r.Use(otelgin.Middleware("api"))
		r.Use(middleware.TracingProvider(config))
	}

	// DBs
	r.Use(func(g *gin.Context) { middleware.Provider("DB", db.WithContext(g.Request.Context()))(g) }) //nolint:contextcheck
	r.Use(middleware.PaginatedDatabaseProvider(db, middleware.PaginationConfig{}))

	r.Use(middleware.Provider("PubSub", pubsub))
	r.Use(middleware.Provider("Config", config))
	r.Use(middleware.Provider("Ready", ready))
	r.Use(middleware.Provider("Hub", dmrHub))
	r.Use(middleware.Provider("NetScheduler", ns))
	r.Use(middleware.Provider("Version", version))
	r.Use(middleware.Provider("Commit", commit))
	// CORS
	if config.HTTP.CORS.Enabled {
		corsConfig := cors.DefaultConfig()
		corsConfig.AllowCredentials = true
		corsConfig.AllowOrigins = config.HTTP.CORS.Hosts
		r.Use(cors.New(corsConfig))
	}

	// Sessions
	sessionStore := gormSessions.NewStore(db, true, config.GetDerivedSecret())
	r.Use(sessions.Sessions("sessions", sessionStore))
}

func addSetupWizardMiddleware(config *configPkg.Config, r *gin.Engine, token string, configCompleteChan chan any, version, commit string) {
	// Tracing
	if config.Metrics.OTLPEndpoint != "" {
		r.Use(otelgin.Middleware("setupwizard"))
		r.Use(middleware.TracingProvider(config))
	}

	r.Use(middleware.Provider("Config", config))
	r.Use(middleware.Provider("SetupWizard", token))
	r.Use(middleware.Provider("SetupWizardConfigCompleteChan", configCompleteChan))

	// CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowOrigins = []string{"*"}
	r.Use(cors.New(corsConfig))

	// Versioning
	r.Use(middleware.Provider("Version", version))
	r.Use(middleware.Provider("Commit", commit))
}

func CreateSetupWizardRouter(config *configPkg.Config, token string, configCompleteChan chan any, version, commit string) *gin.Engine {
	r := gin.New()
	// Logging middleware replaced or removed; consider using slog for access logs if needed
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	addSetupWizardMiddleware(config, r, token, configCompleteChan, version, commit)

	setupwizard.ApplyRoutes(config, r)

	addFrontendRoutes(r)

	return r
}

func CreateRouter(ctx context.Context, config *configPkg.Config, dmrHub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, ready *atomic.Bool, ns *netscheduler.NetScheduler, version, commit string) *gin.Engine {
	r := gin.New()
	// Logging middleware replaced or removed; consider using slog for access logs if needed
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	err := r.SetTrustedProxies(config.HTTP.TrustedProxies)
	if err != nil {
		slog.Error("Failed setting trusted proxies", "error", err)
	}

	addMiddleware(ctx, config, dmrHub, r, db, pubsub, ready, ns, version, commit)

	ratelimitStore := gormRateLimit.NewGORMStore(&gormRateLimit.GORMOptions{
		DB:    db,
		Rate:  rateLimitRate,
		Limit: rateLimitLimit,
	})
	ratelimitMW := ratelimit.RateLimiter(ratelimitStore, &ratelimit.Options{
		ErrorHandler: func(c *gin.Context, info ratelimit.Info) {
			c.String(http.StatusTooManyRequests, "Too many requests. Try again in "+time.Until(info.ResetTime).String())
		},
		KeyFunc: func(c *gin.Context) string {
			return c.ClientIP()
		},
	})

	userLockoutMiddleware := middleware.SuspendedUserLockout()

	api.ApplyRoutes(config, r, dmrHub, db, pubsub, ratelimitMW, userLockoutMiddleware)

	addFrontendRoutes(r)

	return r
}
func (s *Server) Stop(ctx context.Context) {
	s.stopOnce.Do(func() {
		slog.Info("Stopping HTTP Server")
		const timeout = 5 * time.Second
		shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to shutdown HTTP server", "error", err)
		}
		<-s.shutdownChannel
	})
}

var ErrClosed = errors.New("server closed")
var ErrFailed = errors.New("failed to start server")

func (s *Server) Start() error {
	go func() {
		err := s.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Failed to start HTTP server", "error", err)
		}
		s.shutdownChannel <- true
	}()
	return nil
}
