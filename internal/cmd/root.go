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
	"github.com/USA-RedDragon/DMRHub/internal/logging"
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

	c, err := configulator.FromContext[config.Config](ctx)
	if err != nil {
		return fmt.Errorf("failed to get config from context: %w", err)
	}

	cfg, err := c.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

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

	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return fmt.Errorf("failed to create scheduler: %w", err)
	}

	var cleanup func(context.Context) error
	if cfg.Metrics.OTLPEndpoint != "" {
		cleanup = initTracer(cfg)
		defer func() {
			err := cleanup(ctx)
			if err != nil {
				slog.Error("Failed to shutdown tracer", "error", err)
			}
		}()
	}
	go metrics.CreateMetricsServer(cfg)
	go pprof.CreatePProfServer(cfg)

	database, err := db.MakeDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Dummy call to get the data decoded into memory early
	go func() {
		err := repeaterdb.Update()
		if err != nil {
			slog.Error("Failed to update repeater database, using built in one", "error", err)
		}
	}()
	_, err = scheduler.NewJob(
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
		logging.Errorf("Failed to schedule repeater update: %s", err)
	}

	go func() {
		err = userdb.Update()
		if err != nil {
			logging.Errorf("Failed to update user database: %s using built in one", err)
		}
	}()
	_, err = scheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(
			gocron.NewAtTime(0, 0, 0),
		)),
		gocron.NewTask(func() {
			err := userdb.Update()
			if err != nil {
				logging.Errorf("Failed to update user database: %s", err)
			}
		}),
	)
	if err != nil {
		logging.Errorf("Failed to schedule user update: %s", err)
	}

	scheduler.Start()

	// const connsPerCPU = 10
	// const maxIdleTime = 10 * time.Minute

	// TODO: move this to pubsub and kv packages
	// if cfg.Redis.Enabled {
	// 	redis := redis.NewClient(&redis.Options{
	// 		Addr:            fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
	// 		Password:        cfg.Redis.Password,
	// 		PoolFIFO:        true,
	// 		PoolSize:        runtime.GOMAXPROCS(0) * connsPerCPU,
	// 		MinIdleConns:    runtime.GOMAXPROCS(0),
	// 		ConnMaxIdleTime: maxIdleTime,
	// 	})
	// 	_, err = redis.Ping(ctx).Result()
	// 	if err != nil {
	// 		return fmt.Errorf("failed to connect to redis: %w", err)
	// 	}
	// 	defer func() {
	// 		err := redis.Close()
	// 		if err != nil {
	// 			slog.Error("Failed to close redis connection", "error", err)
	// 		}
	// 	}()
	// 	if cfg.Metrics.OTLPEndpoint != "" {
	// 		if err := redisotel.InstrumentTracing(redis); err != nil {
	// 			return fmt.Errorf("failed to trace redis: %w", err)
	// 		}

	// 		// Enable metrics instrumentation.
	// 		if err := redisotel.InstrumentMetrics(redis); err != nil {
	// 			return fmt.Errorf("failed to instrument redis metrics: %w", err)
	// 		}
	// 	}
	// }

	kv, err := kv.MakeKV(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to key-value store: %w", err)
	}

	pubsub, err := pubsub.MakePubSub(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to pubsub: %w", err)
	}

	callTracker := calltracker.NewCallTracker(database, pubsub)

	hbrpServer := hbrp.MakeServer(cfg, database, pubsub, kv, callTracker, cmd.Annotations["version"], cmd.Annotations["commit"])
	err = hbrpServer.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start hbrp server: %w", err)
	}
	defer hbrpServer.Stop(ctx)

	g := new(errgroup.Group)
	g.Go(func() error {
		// For each repeater in the DB, start a gofunc to listen for calls
		repeaters, err := models.ListRepeaters(database)
		if err != nil {
			return fmt.Errorf("failed to list repeaters: %w", err)
		}
		for _, repeater := range repeaters {
			go hbrp.GetSubscriptionManager(database).ListenForCalls(pubsub, repeater.ID)
		}
		return nil
	})

	if cfg.DMR.OpenBridge.Enabled {
		// Start the OpenBridge server
		openbridgeServer := openbridge.MakeServer(cfg, database, pubsub, kv, callTracker)
		err := openbridgeServer.Start(ctx)
		if err != nil {
			return fmt.Errorf("failed to start OpenBridge server: %w", err)
		}
		defer openbridgeServer.Stop(ctx)

		go func() {
			// For each peer in the DB, start a gofunc to listen for calls
			peers := models.ListPeers(database)
			for _, peer := range peers {
				go openbridge.GetSubscriptionManager().Subscribe(ctx, pubsub, peer)
			}
		}()
	}

	http := http.MakeServer(cfg, database, pubsub, cmd.Annotations["version"], cmd.Annotations["commit"])
	err = http.Start()
	if err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}
	defer http.Stop()

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to start repeater listeners: %w", err)
	}

	stop := func(sig os.Signal) {
		slog.Error("Shutting down due to signal", "signal", sig)
		wg := new(sync.WaitGroup)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			err = scheduler.StopJobs()
			if err != nil {
				logging.Errorf("Failed to stop scheduler jobs: %s", err)
			}
			err = scheduler.Shutdown()
			if err != nil {
				logging.Errorf("Failed to stop scheduler: %s", err)
			}
		}(wg)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			hbrp.GetSubscriptionManager(database).CancelAllSubscriptions()
			hbrpServer.Stop(ctx)
		}(wg)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			if cfg.Metrics.OTLPEndpoint != "" {
				const timeout = 5 * time.Second
				ctx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				err := cleanup(ctx)
				if err != nil {
					logging.Errorf("Failed to shutdown tracer: %s", err)
				}
			}
		}(wg)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			http.Stop()
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
			err = pubsub.Close()
			if err != nil {
				logging.Errorf("Failed to close pubsub: %s", err)
			}
			err = kv.Close()
			if err != nil {
				logging.Errorf("Failed to close kv: %s", err)
			}
			logging.Error("Shutdown safely completed")
			logging.Close()
			os.Exit(0)
		case <-time.After(timeout):
			logging.Error("Shutdown timed out")
			logging.Close()
			os.Exit(1)
		}
	}
	defer stop(syscall.SIGINT)

	shutdown.AddWithParam(stop)

	shutdown.Listen(syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	return nil
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
		logging.Errorf("Failed tracing app: %v", err)
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "DMRHub"),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		logging.Errorf("Could not set resources: %v", err)
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
