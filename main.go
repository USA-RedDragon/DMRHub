package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/go-co-op/gocron"

	"github.com/USA-RedDragon/dmrserver-in-a-box/config"
	"github.com/USA-RedDragon/dmrserver-in-a-box/dmr"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http"
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/USA-RedDragon/dmrserver-in-a-box/repeaterdb"
	"github.com/USA-RedDragon/dmrserver-in-a-box/sdk"
	"github.com/USA-RedDragon/dmrserver-in-a-box/userdb"
	"k8s.io/klog/v2"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var scheduler = gocron.NewScheduler(time.UTC)

func main() {
	rand.Seed(time.Now().UnixNano())
	defer klog.Flush()
	klog.Infof("DMR Network in a box v%s-%s", sdk.Version, sdk.GitCommit)

	db, err := gorm.Open(postgres.Open(config.GetConfig().PostgresDSN), &gorm.Config{})
	if err != nil {
		klog.Errorf("Failed to open database: %s", err)
	}
	db.AutoMigrate(&models.Call{}, &models.Repeater{}, &models.Talkgroup{}, &models.User{})
	if db.Error != nil {
		//We have an error
		klog.Exitf(fmt.Sprintf("Failed with error %s", db.Error))
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

	dmrServer := dmr.MakeServer(db)
	dmrServer.Listen()
	defer dmrServer.Stop()

	// For each repeater in the DB, start a gofunc to listen for calls
	repeaters := models.ListRepeaters(db)
	for _, repeater := range repeaters {
		klog.Infof("Starting repeater %s", repeater.RadioID)
		go repeater.ListenForCalls()
	}

	http.Start(db)
}
