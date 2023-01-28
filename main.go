package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/USA-RedDragon/dmrserver-in-a-box/config"
	"github.com/USA-RedDragon/dmrserver-in-a-box/dmr"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http"
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/USA-RedDragon/dmrserver-in-a-box/repeaterdb"
	"github.com/USA-RedDragon/dmrserver-in-a-box/sdk"
	"github.com/USA-RedDragon/dmrserver-in-a-box/userdb"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"k8s.io/klog/v2"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var scheduler = gocron.NewScheduler(time.UTC)

func initTracer() func(context.Context) error {
	exporter, err := otlptrace.New(
		context.Background(),
		otlptracegrpc.NewClient(
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithEndpoint(config.GetConfig().OTLPEndpoint),
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	resources, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "dmrserver-in-a-box"),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		log.Printf("Could not set resources: ", err)
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
	rand.Seed(time.Now().UnixNano())
	defer klog.Flush()

	ctx := context.Background()

	cleanup := initTracer()
	defer cleanup(ctx)

	klog.Infof("DMR Network in a box v%s-%s", sdk.Version, sdk.GitCommit)

	db, err := gorm.Open(postgres.Open(config.GetConfig().PostgresDSN), &gorm.Config{})
	if err != nil {
		klog.Exitf("Failed to open database: %s", err)
		return
	}
	if err := db.Use(otelgorm.NewPlugin()); err != nil {
		klog.Exitf("Failed to trace database: %s", err)
		return
	}
	db.AutoMigrate(&models.Call{}, &models.Repeater{}, &models.Talkgroup{}, &models.User{})
	if db.Error != nil {
		//We have an error
		klog.Exitf(fmt.Sprintf("Failed with error %s", db.Error))
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		klog.Exitf("Failed to open database: %s", err)
		return
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(-1)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Dummy call to get the data decoded into memory early
	go func() {
		repeaterdb.GetDMRRepeaters()
		err = repeaterdb.Update()
		if err != nil {
			klog.Errorf("Failed to update repeater database: %s using built in one", err)
		}
	}()
	scheduler.Every(1).Day().At("00:00").Do(func() {
		err = repeaterdb.Update()
		if err != nil {
			klog.Errorf("Failed to update repeater database: %s", err)
		}
	})

	go func() {
		userdb.GetDMRUsers()
		err = userdb.Update()
		if err != nil {
			klog.Errorf("Failed to update user database: %s using built in one", err)
		}
	}()
	scheduler.Every(1).Day().At("00:00").Do(func() {
		err = userdb.Update()
		if err != nil {
			klog.Errorf("Failed to update repeater database: %s", err)
		}
	})

	scheduler.StartAsync()

	redis := redis.NewClient(&redis.Options{
		Addr: config.GetConfig().RedisHost,
	})
	_, err = redis.Ping(ctx).Result()
	if err != nil {
		klog.Errorf("Failed to connect to redis: %s", err)
		return
	}
	defer redis.Close()
	if err := redisotel.InstrumentTracing(redis); err != nil {
		klog.Errorf("Failed to trace redis: %s", err)
		return
	}

	// Enable metrics instrumentation.
	if err := redisotel.InstrumentMetrics(redis); err != nil {
		klog.Errorf("Failed to instrument redis: %s", err)
		return
	}

	dmrServer := dmr.MakeServer(db, redis)
	dmrServer.Listen(ctx)
	defer dmrServer.Stop(ctx)

	// For each repeater in the DB, start a gofunc to listen for calls
	repeaters := models.ListRepeaters(db)
	for _, repeater := range repeaters {
		klog.Infof("Starting repeater %s", repeater.RadioID)
		go repeater.ListenForCalls(ctx, redis)
	}

	http.Start(db, redis)
}
