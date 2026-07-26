// Command server runs the Bounty Board HTTP API.
package main

import (
	"context"
	"log/slog"
	"os"

	"bountyboard"
	"bountyboard/internal/logging"
	"bountyboard/internal/store"
)

func main() {
	logging.Setup(os.Getenv("LOG_LEVEL"))

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	db, err := store.Connect(context.Background(), dsn)
	if err != nil {
		slog.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(context.Background(), bountyboard.MigrationFS); err != nil {
		slog.Error("migrate", "error", err)
		os.Exit(1)
	}

	slog.Info("server starting")
}
