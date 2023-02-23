// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

package main

import (
	"context"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/hbrp"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/USA-RedDragon/DMRHub/internal/repeaterdb"
	"github.com/USA-RedDragon/DMRHub/internal/sdk"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/go-co-op/gocron"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	_ "github.com/tinylib/msgp/printer"
	"github.com/ztrue/shutdown"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/sync/errgroup"
)

func initTracer() func(context.Context) error {
	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(config.GetConfig().OTLPEndpoint),
		),
	)
	if err != nil {
		logging.GetLogger(logging.Error).Logf(initTracer, "Failed tracing app: %v", err)
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "DMRHub"),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		logging.GetLogger(logging.Error).Logf(initTracer, "Could not set resources: %v", err)
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

func main() {
	os.Exit(start())
}

func start() int {
	logging.GetLogger(logging.Error).Logf(main, "DMRHub v%s-%s", sdk.Version, sdk.GitCommit)
	logging.GetLogger(logging.Access).Logf(main, "DMRHub v%s-%s", sdk.Version, sdk.GitCommit)
	defer logging.Close()

	ctx := context.Background()

	scheduler := gocron.NewScheduler(time.UTC)

	var cleanup func(context.Context) error
	if config.GetConfig().OTLPEndpoint != "" {
		cleanup = initTracer()
		defer func() {
			err := cleanup(ctx)
			if err != nil {
				logging.GetLogger(logging.Error).Logf(start, "Failed to shutdown tracer: %s", err)
			}
		}()
	}

	database := db.MakeDB()

	// Dummy call to get the data decoded into memory early
	go func() {
		err := repeaterdb.Update()
		if err != nil {
			logging.GetLogger(logging.Error).Logf(start, "Failed to update repeater database: %s using built in one", err)
		}
	}()
	_, err := scheduler.Every(1).Day().At("00:00").Do(func() {
		err := repeaterdb.Update()
		if err != nil {
			logging.GetLogger(logging.Error).Logf(start, "Failed to update repeater database: %s", err)
		}
	})
	if err != nil {
		logging.GetLogger(logging.Error).Logf(start, "Failed to schedule repeater update: %s", err)
	}

	go func() {
		err = userdb.Update()
		if err != nil {
			logging.GetLogger(logging.Error).Logf(start, "Failed to update user database: %s using built in one", err)
		}
	}()
	_, err = scheduler.Every(1).Day().At("00:00").Do(func() {
		err = userdb.Update()
		if err != nil {
			logging.GetLogger(logging.Error).Logf(start, "Failed to update repeater database: %s", err)
		}
	})
	if err != nil {
		logging.GetLogger(logging.Error).Logf(start, "Failed to schedule user update: %s", err)
	}

	scheduler.StartAsync()

	const connsPerCPU = 10
	const maxIdleTime = 10 * time.Minute

	redis := redis.NewClient(&redis.Options{
		Addr:            config.GetConfig().RedisHost,
		Password:        config.GetConfig().RedisPassword,
		PoolFIFO:        true,
		PoolSize:        runtime.GOMAXPROCS(0) * connsPerCPU,
		MinIdleConns:    runtime.GOMAXPROCS(0),
		ConnMaxIdleTime: maxIdleTime,
	})
	_, err = redis.Ping(ctx).Result()
	if err != nil {
		logging.GetLogger(logging.Error).Logf(start, "Failed to connect to redis: %s", err)
		return 1
	}
	defer func() {
		err := redis.Close()
		if err != nil {
			logging.GetLogger(logging.Error).Logf(start, "Failed to close redis: %s", err)
		}
	}()
	if config.GetConfig().OTLPEndpoint != "" {
		if err := redisotel.InstrumentTracing(redis); err != nil {
			logging.GetLogger(logging.Error).Logf(start, "Failed to trace redis: %s", err)
			return 1
		}

		// Enable metrics instrumentation.
		if err := redisotel.InstrumentMetrics(redis); err != nil {
			logging.GetLogger(logging.Error).Logf(start, "Failed to instrument redis: %s", err)
			return 1
		}
	}

	callTracker := calltracker.NewCallTracker(database, redis)

	hbrpServer := hbrp.MakeServer(database, redis, callTracker)
	hbrpServer.Start(ctx)
	defer hbrpServer.Stop(ctx)

	g := new(errgroup.Group)
	g.Go(func() error {
		// For each repeater in the DB, start a gofunc to listen for calls
		repeaters, err := models.ListRepeaters(database)
		if err != nil {
			return err //nolint:golint,wrapcheck
		}
		for _, repeater := range repeaters {
			go hbrp.GetSubscriptionManager().ListenForCalls(ctx, redis, repeater)
		}
		return nil
	})

	http := http.MakeServer(database, redis)
	http.Start()
	defer http.Stop()

	if err := g.Wait(); err != nil {
		logging.GetLogger(logging.Error).Logf(start, "Failed to start repeater listeners: %s", err)
		return 1
	}

	stop := func(sig os.Signal) {
		logging.GetLogger(logging.Error).Logf(start, "Shutting down due to %v", sig)
		wg := new(sync.WaitGroup)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			scheduler.Stop()
		}(wg)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			hbrp.GetSubscriptionManager().CancelAllSubscriptions()
			hbrpServer.Stop(ctx)
		}(wg)

		wg.Add(1)
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			if config.GetConfig().OTLPEndpoint != "" {
				const timeout = 5 * time.Second
				ctx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				_ = cleanup(ctx)
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
			redis.Close()
			logging.GetLogger(logging.Error).Log(start, "Shutdown safely completed")
			logging.Close()
			os.Exit(0)
		case <-time.After(timeout):
			logging.GetLogger(logging.Error).Log(start, "Shutdown timed out")
			logging.Close()
			os.Exit(1)
		}
	}
	defer stop(syscall.SIGINT)

	shutdown.AddWithParam(stop)

	shutdown.Listen(syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)

	return 0
}
