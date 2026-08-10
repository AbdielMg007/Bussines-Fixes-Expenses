package main

import (
	"context"
	"log"

	"runway/backend/internal/config"
	"runway/backend/internal/postgres"
	"runway/backend/migrations"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	databaseURL, err := config.RequireDatabaseURL()
	if err != nil {
		return err
	}
	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.ApplyMigrations(ctx, pool, migrations.Files); err != nil {
		return err
	}
	log.Print("database migrations applied")
	return nil
}
