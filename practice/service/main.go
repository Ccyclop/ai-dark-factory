// Command practice is the inventory reservation service.
package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

const (
	listenAddr = ":8080"
	dbPath     = "/data/practice.db"
)

func main() {
	path := dbPath
	if p := os.Getenv("PRACTICE_DB_PATH"); p != "" {
		path = p // development override; the contracted run command sets nothing
	}

	db, err := openDB(path, true)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	srv := &http.Server{
		Addr:                         listenAddr,
		Handler:                      newServer(db),
		ReadHeaderTimeout:            10 * time.Second,
		ReadTimeout:                  60 * time.Second,
		WriteTimeout:                 60 * time.Second,
		IdleTimeout:                  120 * time.Second,
		MaxHeaderBytes:               1 << 20,
		DisableGeneralOptionsHandler: true, // "OPTIONS *" must reach our JSON handler
	}

	log.Printf("listening on %s (database %s)", listenAddr, path)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
