package main

import (
	"flag"

	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/ui"
)

func main() {
	var endpoint string
	flag.StringVar(&endpoint, "endpoint", "ws://127.0.0.1:12001", "websocket endpoint")
	flag.StringVar(&endpoint, "e", "ws://127.0.0.1:12001", "websocket endpoint (shorthand)")
	flag.Parse()

	initial := telemetry.KindLogs // default; let cli flags adjust if you like
	if err := ui.Run(endpoint, initial); err != nil {
		panic(err)
	}
}
