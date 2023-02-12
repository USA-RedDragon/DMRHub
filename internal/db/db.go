package db

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/glebarez/sqlite"
	gorm_seeder "github.com/kachit/gorm-seeder"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

func MakeDB() *gorm.DB {
	var db *gorm.DB
	var err error
	if os.Getenv("TEST") != "" {
		klog.Info("Using in-memory database for testing")
		db, err = gorm.Open(sqlite.Open(""), &gorm.Config{})
		if err != nil {
			klog.Fatalf("Could not open database: %s", err)
		}
	} else {
		db, err = gorm.Open(postgres.Open(config.GetConfig().PostgresDSN), &gorm.Config{})
		if err != nil {
			klog.Fatalf("Failed to open database: %s", err)
		}
		if config.GetConfig().OTLPEndpoint != "" {
			if err = db.Use(otelgorm.NewPlugin()); err != nil {
				klog.Fatalf("Failed to trace database: %s", err)
			}
		}
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
