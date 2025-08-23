// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"sync"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/hbrp"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pprof"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/USA-RedDragon/configulator"
	"github.com/go-co-op/gocron/v2"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/ztrue/shutdown"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/sync/errgroup"
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

	cleanup := setupTracing(cfg)
	defer func() {
		if cleanup != nil {
			if err := cleanup(ctx); err != nil {
				slog.Error("Failed to shutdown tracer", "error", err)
			}
		}
	}()

	startBackgroundServices(cfg)

	scheduler, err := setupScheduler()
	if err != nil {
		return err
	}

	setupDMRDatabaseJobs(scheduler)

	scheduler.Start()

	if err := cfg.Validate(); err != nil {
		// Validation failed, we need to still run the server to
		// allow the user to fix the config
		slog.Info("Configuration validation failed", "error", err)
		err = waitForConfig(cfg, cmd.Annotations["version"], cmd.Annotations["commit"])
		if err != nil {
			return fmt.Errorf("failed during configuration wait: %w", err)
		}
	}

	database, err := db.MakeDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	servers, err := initializeServers(ctx, cfg, database, cmd.Annotations["version"], cmd.Annotations["commit"])
	if err != nil {
		return err
	}
	defer servers.shutdown(ctx)

	if err := servers.startRepeaterListeners(database); err != nil {
		return err
	}

	setupShutdownHandlers(ctx, scheduler, servers, cleanup)

	return nil
}

// waitForConfig waits for the user to fix the config file
func waitForConfig(config *config.Config, version, commit string) error {
	slog.Info("Starting a setup wizard at http://localhost:3005/setup")
	httpServer := http.MakeSetupWizardServer(config, version, commit)
	err := httpServer.Start()
	if err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	stop := func(sig os.Signal) {
		slog.Error("Shutting down due to signal", "signal", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Stop(ctx)
		os.Exit(0)
	}

	shutdown.AddWithParam(stop)

	ch := make(chan any, 1)
	go func() {
		for {
			time.Sleep(1 * time.Second)
			if err := config.Validate(); err == nil {
				slog.Info("Configuration is valid, shutting down setup wizard")
				httpServer.Stop(context.Background())
				ch <- struct{}{}
				return
			}
		}
	}()
	go func() {
		<-ch
		shutdown.Reset()
	}()

	shutdown.Listen(syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	return nil
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
func setupDMRDatabaseJobs(scheduler gocron.Scheduler) {
	// Dummy call to get the data decoded into memory early
	go func() {
		err := repeaterdb.Update()
		if err != nil {
			slog.Error("Failed to update repeater database, using built in one", "error", err)
		}
	}()

	_, err := scheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(
			gocron.NewAtTime(0, 0, 0),
		)),
		gocron.NewTask(func() {
			err := repeaterdb.Update()
			if err != nil {
				slog.Error("Failed to update repeater database", "error", err)
			}
		}),
	)
	if err != nil {
		slog.Error("Failed to schedule repeater update", "error", err)
	}

	go func() {
		err := userdb.Update()
		if err != nil {
			slog.Error("Failed to update user database", "error", err)
		}
	}()

	_, err = scheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(
			gocron.NewAtTime(0, 0, 0),
		)),
		gocron.NewTask(func() {
			err := userdb.Update()
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
	hbrpServer       hbrp.Server
	openbridgeServer *openbridge.Server
	httpServer       http.Server
	kv               kv.KV
	pubsub           pubsub.PubSub
	database         *gorm.DB
	cfg              *config.Config
}

// shutdown gracefully stops all servers
func (sm *serverManager) shutdown(ctx context.Context) {
	sm.hbrpServer.Stop(ctx)
	if sm.openbridgeServer != nil {
		sm.openbridgeServer.Stop(ctx)
	}
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

// startRepeaterListeners starts listeners for all repeaters and peers
func (sm *serverManager) startRepeaterListeners(database *gorm.DB) error {
	g := new(errgroup.Group)
	g.Go(func() error {
		// For each repeater in the DB, start a gofunc to listen for calls
		repeaters, err := models.ListRepeaters(database)
		if err != nil {
			return fmt.Errorf("failed to list repeaters: %w", err)
		}
		for _, repeater := range repeaters {
			go hbrp.GetSubscriptionManager(database).ListenForCalls(sm.pubsub, repeater.ID)
		}
		return nil
	})

	if sm.cfg.DMR.OpenBridge.Enabled {
		go func() {
			// For each peer in the DB, start a gofunc to listen for calls
			peers := models.ListPeers(database)
			for _, peer := range peers {
				go openbridge.GetSubscriptionManager().Subscribe(context.Background(), sm.pubsub, peer)
			}
		}()
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to start repeater listeners: %w", err)
	}

	return nil
}

// initializeServers creates and starts all server instances
func initializeServers(ctx context.Context, cfg *config.Config, database *gorm.DB, version, commit string) (*serverManager, error) {
	kvStore, err := kv.MakeKV(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to key-value store: %w", err)
	}

	pubsubClient, err := pubsub.MakePubSub(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to pubsub: %w", err)
	}

	callTracker := calltracker.NewCallTracker(database, pubsubClient)

	sm := &serverManager{
		kv:       kvStore,
		pubsub:   pubsubClient,
		database: database,
		cfg:      cfg,
	}

	hbrpServer := hbrp.MakeServer(cfg, database, pubsubClient, kvStore, callTracker, version, commit)
	err = hbrpServer.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start hbrp server: %w", err)
	}
	sm.hbrpServer = hbrpServer

	if cfg.DMR.OpenBridge.Enabled {
		openbridgeServer := openbridge.MakeServer(cfg, database, pubsubClient, kvStore, callTracker)
		err := openbridgeServer.Start(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to start OpenBridge server: %w", err)
		}
		sm.openbridgeServer = &openbridgeServer
	}

	httpServer := http.MakeServer(cfg, database, pubsubClient, version, commit)
	err = httpServer.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start HTTP server: %w", err)
	}
	sm.httpServer = httpServer

	return sm, nil
}

// setupShutdownHandlers configures graceful shutdown handlers
func setupShutdownHandlers(ctx context.Context, scheduler gocron.Scheduler, servers *serverManager, cleanup func(context.Context) error) {
	stop := func(sig os.Signal) {
		slog.Error("Shutting down due to signal", "signal", sig)
		wg := new(sync.WaitGroup)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			err := scheduler.StopJobs()
			if err != nil {
				slog.Error("Failed to stop scheduler jobs", "error", err)
			}
			err = scheduler.Shutdown()
			if err != nil {
				slog.Error("Failed to stop scheduler", "error", err)
			}
		}(wg)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			hbrp.GetSubscriptionManager(servers.database).CancelAllSubscriptions()
			servers.shutdown(ctx)
		}(wg)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
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
		}(wg)

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
	defer stop(syscall.SIGINT)

	shutdown.AddWithParam(stop)

	shutdown.Listen(syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
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
