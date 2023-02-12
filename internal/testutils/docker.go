package testutils

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	gorm_seeder "github.com/kachit/gorm-seeder"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/glebarez/sqlite"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

var client *redis.Client
var db *gorm.DB
var redisContainer *dockertest.Resource

func CreateRedis() *redis.Client {
	if client != nil {
		return client
	}
	pool, err := dockertest.NewPool("")
	if err != nil {
		klog.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		klog.Fatalf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	redisContainer, err = pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "redis",
		Tag:        "7-alpine",
		Cmd:        []string{"--requirepass", "password"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"6379/tcp": {
				{
					HostIP:   "127.0.0.1",
					HostPort: "6379",
				},
			},
		},
	})
	if err != nil {
		klog.Fatalf("Could not start resource: %s", err)
	}

	client = redis.NewClient(&redis.Options{
		Addr:            "127.0.0.1:6379",
		Password:        "password",
		PoolFIFO:        true,
		PoolSize:        runtime.GOMAXPROCS(0) * 10,
		MinIdleConns:    runtime.GOMAXPROCS(0),
		ConnMaxIdleTime: 10 * time.Minute,
	})
	_, err = client.Ping(context.Background()).Result()
	if err != nil {
		klog.Fatalf("Failed to connect to redis: %s", err)
	}
	return client
}

func CreateDB() *gorm.DB {
	if db != nil {
		return db
	}
	var err error
	db, err = gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		klog.Fatalf("Could not open database: %s", err)
	}

	err = db.AutoMigrate(&models.AppSettings{})
	if err != nil {
		klog.Fatalf("Failed to migrate database: %s", err)
	}
	if db.Error != nil {
		//We have an error
		klog.Fatalf(fmt.Sprintf("Failed with error %s", db.Error))
	}

InitDB:
	// Grab the first (and only) AppSettings record. If that record doesn't exist, create it.
	var appSettings models.AppSettings
	result := db.First(&appSettings)
	if result.Error != nil {
		// We have an error
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			if config.GetConfig().Debug {
				klog.Infof("App settings entry doesn't exist, migrating db and creating it")
			}
			// The record doesn't exist, so create it
			appSettings = models.AppSettings{
				HasSeeded: false,
			}
			err = db.AutoMigrate(&models.Call{}, &models.Repeater{}, &models.Talkgroup{}, &models.User{})
			if err != nil {
				klog.Fatalf("Failed to migrate database: %s", err)
			}
			if db.Error != nil {
				//We have an error
				klog.Fatalf(fmt.Sprintf("Failed with error %s", db.Error))
			}
			db.Create(&appSettings)
			if config.GetConfig().Debug {
				klog.Infof("App settings saved")
			}
		} else if strings.HasPrefix(result.Error.Error(), "ERROR: relation \"app_settings\" does not exist") {
			if config.GetConfig().Debug {
				klog.Infof("App settings table doesn't exist, creating it")
			}
			err = db.AutoMigrate(&models.AppSettings{})
			if err != nil {
				klog.Fatalf("Failed to migrate database with AppSettings: %s", err)
			}
			if db.Error != nil {
				//We have an error
				klog.Fatalf(fmt.Sprintf("Failed to migrate database with AppSettings: %s", db.Error))
			}
			goto InitDB
		} else {
			// We have an error
			klog.Fatalf(fmt.Sprintf("App settings save failed with error %s", result.Error))
		}
	}

	// If the record exists and HasSeeded is true, then we don't need to seed the database.
	if !appSettings.HasSeeded {
		usersSeeder := models.NewUsersSeeder(gorm_seeder.SeederConfiguration{Rows: 2})
		talkgroupsSeeder := models.NewTalkgroupsSeeder(gorm_seeder.SeederConfiguration{Rows: 1})
		seedersStack := gorm_seeder.NewSeedersStack(db)
		seedersStack.AddSeeder(&usersSeeder)
		seedersStack.AddSeeder(&talkgroupsSeeder)

		//Apply seed
		err = seedersStack.Seed()
		if err != nil {
			klog.Fatalf("Failed to seed database: %s", err)
		}
		appSettings.HasSeeded = true
		db.Save(&appSettings)
	}

	sqlDB, err := db.DB()
	if err != nil {
		klog.Fatalf("Failed to open database: %s", err)
	}
	sqlDB.SetMaxIdleConns(runtime.GOMAXPROCS(0))
	sqlDB.SetMaxOpenConns(runtime.GOMAXPROCS(0) * 10)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	return db
}

func CloseRedis() {
	if client != nil {
		_ = client.Close()
	}
	if redisContainer != nil {
		_ = redisContainer.Close()
	}
}

func CloseDB() {
	if db != nil {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	}
}
