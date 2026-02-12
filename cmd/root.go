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

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/ipsc"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/mmdvm"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pprof"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/USA-RedDragon/configulator"
	"github.com/go-co-op/gocron/v2"
	"github.com/lmittmann/tint"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"gorm.io/gorm"
)

func NewCommand(version, commit string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "DMRHub",
		Version: fmt.Sprintf("%s - %s", version, commit),
		Annotations: map[string]string{
			"version": version,
			"commit":  commit,
		},
		RunE:              runRoot,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}
	return cmd
}

func runRoot(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	fmt.Printf("DMRHub - %s (%s)\n", cmd.Annotations["version"], cmd.Annotations["commit"])

	cfg, err := loadConfig(ctx)
	if err != nil {
		return err
	}

	setupLogger(cfg)

	scheduler, err := setupScheduler()
	if err != nil {
		return err
	}

	setupDMRDatabaseJobs(cfg, scheduler)

	scheduler.Start()

	if err := cfg.Validate(); err != nil {
		// Validation failed, we need to still run the server to
		// allow the user to fix the config
		slog.Info("Configuration validation failed", "error", err)
		exit := waitForConfig(cfg, cmd.Annotations["version"], cmd.Annotations["commit"])
		if exit {
			return nil
		}
	}

	setupLogger(cfg)

	cleanup := setupTracing(cfg)
	defer func() {
		if cleanup != nil {
			if err := cleanup(ctx); err != nil {
				slog.Error("Failed to shutdown tracer", "error", err)
			}
		}
	}()

	startBackgroundServices(cfg)

	database, err := db.MakeDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	kvStore, err := kv.MakeKV(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to key-value store: %w", err)
	}

	pubsubClient, err := pubsub.MakePubSub(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to pubsub: %w", err)
	}

	callTracker := calltracker.NewCallTracker(database, pubsubClient)

	dmrHub := hub.NewHub(database, kvStore, pubsubClient, callTracker)
	dmrHub.Start()

	servers, err := initializeServers(ctx, cfg, dmrHub, kvStore, pubsubClient, database, cmd.Annotations["version"], cmd.Annotations["commit"])
	if err != nil {
		return err
	}
	defer servers.shutdown(ctx)

	setupShutdownHandlers(ctx, scheduler, dmrHub, servers, cleanup)

	return nil
}

// waitForConfig waits for the user to fix the config file
func waitForConfig(config *config.Config, version, commit string) (exit bool) {
	slog.Info("Setup can be completed in a web browser")
	token, err := utils.RandomPassword(12, 0, 0)
	if err != nil {
		slog.Error("Failed to generate token", "error", err)
		return
	}
	url := "http://localhost:3005/setup?token=" + token
	slog.Info("Opening setup wizard at " + url)
	configCh := make(chan any, 1)
	httpServer := http.MakeSetupWizardServer(config, token, configCh, version, commit)
	go func() {
		err := httpServer.Start()
		if err != nil {
			if !strings.Contains(err.Error(), "server closed") {
				slog.Error("failed to start HTTP server", "error", err)
			}
		}
	}()

	err = browser.OpenURL(url)
	if err != nil {
		slog.Error("Failed to open browser, please open "+url+" manually", "error", err)
	}

	stop := func(sig os.Signal) {
		slog.Error("Shutting down due to signal", "signal", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Stop(ctx)
		if sig != nil {
			exit = true
		}
	}

	interruptCh := make(chan os.Signal, 1)
	go func() {
		<-configCh
		slog.Info("Setup complete, shutting down setup wizard")
		signal.Stop(interruptCh)
		close(interruptCh)
	}()

	signal.Notify(interruptCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	stop(<-interruptCh)
	return
}

// loadConfig loads the configuration from context
func loadConfig(ctx context.Context) (*config.Config, error) {
	c, err := configulator.FromContext[config.Config](ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get config from context: %w", err)
	}

	cfg, err := c.LoadWithoutValidation()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return cfg, nil
}

// setupLogger configures the structured logger
func setupLogger(cfg *config.Config) {
	var logger *slog.Logger
	switch cfg.LogLevel {
	case config.LogLevelDebug:
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{Level: slog.LevelDebug}))
	case config.LogLevelInfo:
		logger = slog.New(tint.NewHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo}))
	case config.LogLevelWarn:
		logger = slog.New(tint.NewHandler(os.Stderr, &tint.Options{Level: slog.LevelWarn}))
	case config.LogLevelError:
		logger = slog.New(tint.NewHandler(os.Stderr, &tint.Options{Level: slog.LevelError}))
	}
	slog.SetDefault(logger)
}

// setupScheduler creates and configures the job scheduler
func setupScheduler() (gocron.Scheduler, error) {
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduler: %w", err)
	}
	return scheduler, nil
}

// setupTracing initializes OpenTelemetry tracing if configured
func setupTracing(cfg *config.Config) func(context.Context) error {
	if cfg.Metrics.OTLPEndpoint == "" {
		return nil
	}
	return initTracer(cfg)
}

// startBackgroundServices starts metrics and pprof servers
func startBackgroundServices(cfg *config.Config) {
	go metrics.CreateMetricsServer(cfg)
	go pprof.CreatePProfServer(cfg)
}

// setupDMRDatabaseJobs configures scheduled jobs for database updates
func setupDMRDatabaseJobs(cfg *config.Config, scheduler gocron.Scheduler) {
	// Dummy call to get the data decoded into memory early
	go func() {
		err := repeaterdb.Update(cfg.DMR.RepeaterIDURL)
		if err != nil {
			slog.Error("Failed to update repeater database, using built in one", "error", err)
		}
	}()

	_, err := scheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(
			gocron.NewAtTime(0, 0, 0),
		)),
		gocron.NewTask(func() {
			err := repeaterdb.Update(cfg.DMR.RepeaterIDURL)
			if err != nil {
				slog.Error("Failed to update repeater database", "error", err)
			}
		}),
	)
	if err != nil {
		slog.Error("Failed to schedule repeater update", "error", err)
	}

	go func() {
		err := userdb.Update(cfg.DMR.RadioIDURL)
		if err != nil {
			slog.Error("Failed to update user database", "error", err)
		}
	}()

	_, err = scheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(
			gocron.NewAtTime(0, 0, 0),
		)),
		gocron.NewTask(func() {
			err := userdb.Update(cfg.DMR.RadioIDURL)
			if err != nil {
				slog.Error("Failed to update user database", "error", err)
			}
		}),
	)
	if err != nil {
		slog.Error("Failed to schedule user update", "error", err)
	}
}

// serverManager holds all the server instances and their dependencies
type serverManager struct {
	servers    []servers.DMRServer
	httpServer http.Server
	kv         kv.KV
	pubsub     pubsub.PubSub
	database   *gorm.DB
	cfg        *config.Config
}

func (sm *serverManager) addServer(server servers.DMRServer) {
	sm.servers = append(sm.servers, server)
}

func (sm *serverManager) start(ctx context.Context) error {
	for _, server := range sm.servers {
		err := server.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}
	}
	return nil
}

// stopDMRServers sends disconnect messages (MSTCL, deregistration) and
// closes protocol sockets. This must run before hub.Stop() so that
// connected repeaters/peers receive a clean disconnect.
func (sm *serverManager) stopDMRServers(ctx context.Context) {
	for _, server := range sm.servers {
		if err := server.Stop(ctx); err != nil {
			slog.Error("Failed to stop server", "error", err)
		}
	}
}

// closeResources tears down the HTTP server, pubsub, and KV connections.
// Call this after hub.Stop() has cancelled all subscriptions.
func (sm *serverManager) closeResources(ctx context.Context) {
	sm.httpServer.Stop(ctx)
	if sm.pubsub != nil {
		if err := sm.pubsub.Close(); err != nil {
			slog.Error("Failed to close pubsub", "error", err)
		}
	}
	if sm.kv != nil {
		if err := sm.kv.Close(); err != nil {
			slog.Error("Failed to close kv", "error", err)
		}
	}
}

// shutdown gracefully stops all servers
func (sm *serverManager) shutdown(ctx context.Context) {
	sm.stopDMRServers(ctx)
	sm.closeResources(ctx)
}

// initializeServers creates and starts all server instances
func initializeServers(ctx context.Context, cfg *config.Config, hub *hub.Hub, kvStore kv.KV, pubsubClient pubsub.PubSub, database *gorm.DB, version, commit string) (*serverManager, error) {
	sm := &serverManager{
		kv:       kvStore,
		pubsub:   pubsubClient,
		database: database,
		cfg:      cfg,
	}

	mmdvmServer, err := mmdvm.MakeServer(cfg, hub, database, pubsubClient, kvStore, version, commit)
	if err != nil {
		return nil, fmt.Errorf("failed to create MMDVM server: %w", err)
	}
	sm.addServer(&mmdvmServer)

	if cfg.DMR.OpenBridge.Enabled {
		openbridgeServer, err := openbridge.MakeServer(cfg, hub, database, pubsubClient, kvStore)
		if err != nil {
			return nil, fmt.Errorf("failed to create OpenBridge server: %w", err)
		}
		sm.addServer(&openbridgeServer)
	}

	if cfg.DMR.IPSC.Enabled {
		sm.addServer(ipsc.NewIPSCServer(cfg, hub, database))
	}

	if err := sm.start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start servers: %w", err)
	}

	httpServer := http.MakeServer(cfg, hub, database, pubsubClient, version, commit)
	err = httpServer.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start HTTP server: %w", err)
	}
	sm.httpServer = httpServer

	return sm, nil
}

// setupShutdownHandlers configures graceful shutdown handlers.
// It blocks until SIGINT/SIGTERM/SIGQUIT/SIGHUP is received, then
// performs an orderly shutdown: disconnect repeaters/peers first,
// cancel hub subscriptions, tear down resources.
func setupShutdownHandlers(ctx context.Context, scheduler gocron.Scheduler, hub *hub.Hub, servers *serverManager, cleanup func(context.Context) error) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	sig := <-sigCh
	slog.Error("Shutting down due to signal", "signal", sig)

	wg := new(sync.WaitGroup)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := scheduler.StopJobs()
		if err != nil {
			slog.Error("Failed to stop scheduler jobs", "error", err)
		}
		err = scheduler.Shutdown()
		if err != nil {
			slog.Error("Failed to stop scheduler", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		// Send disconnect messages (MSTCL, deregistration) to repeaters/peers
		// BEFORE cancelling hub subscriptions — otherwise hub.Stop() may consume
		// the entire shutdown budget and os.Exit fires before MSTCL is sent.
		servers.stopDMRServers(ctx)
		hub.Stop()
		servers.closeResources(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if cleanup != nil {
			const timeout = 5 * time.Second
			shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			err := cleanup(shutdownCtx)
			if err != nil {
				slog.Error("Failed to shutdown tracer", "error", err)
			}
		}
	}()

	// Wait for all the servers to stop
	const timeout = 10 * time.Second

	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		slog.Info("All servers stopped, shutting down gracefully")
		os.Exit(0)
	case <-time.After(timeout):
		slog.Error("Shutdown timed out, forcing exit")
		os.Exit(1)
	}
}

func initTracer(config *config.Config) func(context.Context) error {
	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(config.Metrics.OTLPEndpoint),
		),
	)
	if err != nil {
		slog.Error("Failed tracing app", "error", err)
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "DMRHub"),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		slog.Error("Could not set resources", "error", err)
	}

	otel.SetTracerProvider(
		sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resources),
		),
	)
	return exporter.Shutdown
}
