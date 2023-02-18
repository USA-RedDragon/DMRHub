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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/hbrp"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/http"
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
	"k8s.io/klog/v2"
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
		klog.Fatal(err)
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "DMRHub"),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		klog.Infof("Could not set resources: ", err)
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
	defer klog.Flush()

	klog.Infof("DMRHub v%s-%s", sdk.Version, sdk.GitCommit)

	ctx := context.Background()

	scheduler := gocron.NewScheduler(time.UTC)

	if config.GetConfig().OTLPEndpoint != "" {
		cleanup := initTracer()
		defer func() {
			err := cleanup(ctx)
			if err != nil {
				klog.Errorf("Failed to shutdown tracer: %s", err)
			}
		}()
	}

	database := db.MakeDB()

	// Dummy call to get the data decoded into memory early
	go func() {
		err := repeaterdb.Update()
		if err != nil {
			klog.Errorf("Failed to update repeater database: %s using built in one", err)
		}
	}()
	_, err := scheduler.Every(1).Day().At("00:00").Do(func() {
		err := repeaterdb.Update()
		if err != nil {
			klog.Errorf("Failed to update repeater database: %s", err)
		}
	})
	if err != nil {
		klog.Errorf("Failed to schedule repeater update: %s", err)
	}

	go func() {
		err = userdb.Update()
		if err != nil {
			klog.Errorf("Failed to update user database: %s using built in one", err)
		}
	}()
	_, err = scheduler.Every(1).Day().At("00:00").Do(func() {
		err = userdb.Update()
		if err != nil {
			klog.Errorf("Failed to update repeater database: %s", err)
		}
	})
	if err != nil {
		klog.Errorf("Failed to schedule user update: %s", err)
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
		klog.Errorf("Failed to connect to redis: %s", err)
		return
	}
	defer func() {
		err := redis.Close()
		if err != nil {
			klog.Errorf("Failed to close redis: %s", err)
		}
	}()
	if config.GetConfig().OTLPEndpoint != "" {
		if err := redisotel.InstrumentTracing(redis); err != nil {
			klog.Errorf("Failed to trace redis: %s", err)
			return
		}

		// Enable metrics instrumentation.
		if err := redisotel.InstrumentMetrics(redis); err != nil {
			klog.Errorf("Failed to instrument redis: %s", err)
			return
		}
	}

	callTracker := calltracker.NewCallTracker(database, redis)

	hbrpServer := hbrp.MakeServer(database, redis, callTracker)
	hbrpServer.Start(ctx)
	defer hbrpServer.Stop(ctx)

	go func() {
		// For each repeater in the DB, start a gofunc to listen for calls
		repeaters := models.ListRepeaters(database)
		for _, repeater := range repeaters {
			go hbrp.GetSubscriptionManager().ListenForCalls(ctx, redis, repeater)
		}
	}()

	if config.GetConfig().OpenBridgePort != 0 {
		// Start the OpenBridge server
		openbridgeServer := openbridge.MakeServer(database, redis, callTracker)
		openbridgeServer.Start(ctx)
		defer openbridgeServer.Stop(ctx)

		go func() {
			// For each peer in the DB, start a gofunc to listen for calls
			peers := models.ListPeers(database)
			for _, peer := range peers {
				go openbridge.GetSubscriptionManager().Subscribe(ctx, redis, peer)
			}
		}()
	}

	http := http.MakeServer(database, redis)
	http.Start()
	defer http.Stop()

	stop := func(sig os.Signal) {
		klog.Infof("Shutting down due to %v", sig)
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
			klog.Info("Shutdown safely completed")
			os.Exit(0)
		case <-time.After(timeout):
			klog.Error("Shutdown timed out")
			os.Exit(1)
		}
	}
	defer stop(syscall.SIGINT)

	shutdown.AddWithParam(stop)

	shutdown.Listen(syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
}
