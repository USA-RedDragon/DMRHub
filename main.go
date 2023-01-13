package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/dmr"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http"
	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"github.com/USA-RedDragon/dmrserver-in-a-box/sdk"
	"github.com/USA-RedDragon/dmrserver-in-a-box/userdb"
	"k8s.io/klog/v2"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var verbose = flag.Bool("verbose", false, "Whether to display verbose logs")

func main() {
	defer klog.Flush()
	klog.Infof("DMR Network in a box v%s-%s", sdk.Version, sdk.GitCommit)
	var redisHost = flag.String("redis", "localhost:6379", "The hostname of redis")
	var listen = flag.String("listen", "0.0.0.0", "The IP to listen on")
	var dmrPort = flag.Int("dmr-port", 62031, "The Port to listen on")
	var frontendPort = flag.Int("frontend-port", 3005, "The Port to listen on")

	flag.Parse()

	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
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
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Dummy call to get the data decoded early
	userdb.GetDMRUsers()

	dmrServer := dmr.MakeServer(*listen, *dmrPort, *redisHost, *verbose, db)
	go dmrServer.Listen()
	defer dmrServer.Stop()

	http.Start(*listen, *frontendPort, *verbose)
}
