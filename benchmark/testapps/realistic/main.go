package main

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"whackbench/testapps/realistic/pkgd"
)

// realistic is the ~5-package scenario: closer to real-world build times
// than the minimal scenario, so the benchmark can show whether a slower
// build amplifies or masks a hot-reload tool's own overhead.
func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:8092")
	if err != nil {
		panic(err)
	}

	svc := pkgd.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		phrase := r.URL.Query().Get("q")
		if phrase == "" {
			phrase = "the quick brown fox jumps over the lazy dog"
		}
		report := svc.Analyze(phrase)
		fmt.Fprintf(w, "build=%d %s\n", BuildMarker, report.String())
	})

	fmt.Printf("READY build=%d ts=%d\n", BuildMarker, time.Now().UnixNano())

	if err := http.Serve(ln, mux); err != nil {
		panic(err)
	}
}
