// fact0-migrate installs the fresh OSS schema or verifies its tracked version.
package main

import (
	"context"
	"github.com/fact0-ai/fact0/internal/platform/config"
	"github.com/fact0-ai/fact0/internal/storage/postgres"
	"log"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := config.Load(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if err := postgres.Migrate(ctx, cfg.Postgres.DSN()); err != nil {
		log.Fatal(err)
	}
	log.Print("Fact0 core schema is up to date")
}
