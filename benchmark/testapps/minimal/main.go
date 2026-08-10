package main

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// minimal is the near-zero-compile-time scenario: a single file, one
// dependency-free HTTP server. It isolates each hot-reload tool's own
// watch/restart overhead from Go compiler speed.
func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:8091")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "build=%d\n", BuildMarker)
	})

	// Printed the instant the listener is bound and the server is about to
	// accept connections - this is the READY signal the benchmark harness
	// scans for on stdout, regardless of which tool is wrapping this process.
	fmt.Printf("READY build=%d ts=%d\n", BuildMarker, time.Now().UnixNano())

	if err := http.Serve(ln, mux); err != nil {
		panic(err)
	}
}
