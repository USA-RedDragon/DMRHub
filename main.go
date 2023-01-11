package main

import (
	"flag"
)

var verbose = flag.Bool("verbose", true, "Whether to display verbose logs")

func main() {
	log("DMR Network in a box")
	var redisHost = flag.String("redis", "localhost:6379", "The hostname of redis")
	var listen = flag.String("listen", "0.0.0.0", "The IP to listen on")
	var port = flag.Int("port", 62031, "The Port to listen on")

	flag.Parse()

	server := makeThreadedUDPServer(*listen, *port, *redisHost)
	server.Listen()
}
