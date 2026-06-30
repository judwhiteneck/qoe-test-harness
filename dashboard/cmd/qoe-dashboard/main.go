// Command qoe-dashboard serves the engineer view (ingest + CDF overlay). It
// defaults to an in-memory store so it runs with no dependencies; point it at
// Postgres by opening a *sql.DB with your driver and passing storage.NewPostgres.
//
// To wire Postgres, add a driver blank-import (e.g. _ "github.com/jackc/pgx/v5/stdlib")
// and replace the store below with:
//
//	db, err := sql.Open("pgx", *dsn)   // apply storage/schema.sql once first
//	store := storage.NewPostgres(db)
//
// We keep the default driver-free so the binary builds without third-party deps.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/judwhiteneck/qoe-test-harness/dashboard"
	"github.com/judwhiteneck/qoe-test-harness/storage"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	store := storage.NewMemory()
	srv := dashboard.New(store)

	log.Printf("qoe-dashboard listening on %s (in-memory store)", *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
