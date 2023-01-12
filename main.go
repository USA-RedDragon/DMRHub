package main

import (
	"flag"

	"github.com/USA-RedDragon/dmrserver-in-a-box/dmr"
	"github.com/USA-RedDragon/dmrserver-in-a-box/http"
	"github.com/USA-RedDragon/dmrserver-in-a-box/sdk"
	"k8s.io/klog/v2"
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

	dmrServer := dmr.MakeServer(*listen, *dmrPort, *redisHost, *verbose)
	go dmrServer.Listen()
	defer dmrServer.Stop()
	http.Start(*listen, *frontendPort)
}
