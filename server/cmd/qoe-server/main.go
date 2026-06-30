// Command qoe-server runs the UDP test endpoint (handshake + probe echo).
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/judwhiteneck/qoe-test-harness/server"
)

func main() {
	addr := flag.String("addr", ":7700", "UDP listen address")
	secret := flag.String("secret", "", "server secret for cookies (required)")
	flag.Parse()
	if *secret == "" {
		log.Fatal("--secret is required")
	}

	srv, err := server.Listen(*addr, []byte(*secret))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer srv.Close()
	log.Printf("qoe-server listening on %s", srv.Addr())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("serve: %v", err)
	}
}
