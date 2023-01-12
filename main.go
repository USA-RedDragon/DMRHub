package main

import (
	"flag"

	"github.com/USA-RedDragon/dmrserver-in-a-box/http"
	"github.com/USA-RedDragon/dmrserver-in-a-box/sdk"
)

var verbose = flag.Bool("verbose", true, "Whether to display verbose logs")

func main() {
	log("DMR Network in a box v%s-%s", sdk.Version, sdk.GitCommit)
	var redisHost = flag.String("redis", "localhost:6379", "The hostname of redis")
	var listen = flag.String("listen", "0.0.0.0", "The IP to listen on")
	var dmrPort = flag.Int("dmr-port", 62031, "The Port to listen on")
	var frontendPort = flag.Int("frontend-port", 3005, "The Port to listen on")

	flag.Parse()

	server := makeThreadedUDPServer(*listen, *dmrPort, *redisHost)
	go server.Listen()
	defer server.Stop()
	http.Start(*listen, *frontendPort)
}
