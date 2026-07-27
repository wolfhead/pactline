// Command server runs the Bounty Board HTTP API.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"bountyboard"
	"bountyboard/internal/api"
	legacyapi "bountyboard/internal/legacy/api"
	legacystore "bountyboard/internal/legacy/store"
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

	users := store.NewUserStore(db)
	tasks := store.NewTaskStore(db)
	comments := store.NewCommentStore(db)
	labels := store.NewLabelStore(db)

	// The bounty/credit/scoring mechanism moved to internal/legacy — see
	// internal/legacy/README.md. Its router is mounted under /api/legacy/ by
	// api.NewRouter, behind the same identity middleware as the rest of the
	// API.
	legacyHandler := legacyapi.NewRouter(
		users,
		legacystore.NewBountyStore(db),
		legacystore.NewCreditStore(db),
		legacystore.NewCalibrationStore(db),
		legacystore.NewAnchorStore(db),
	)

	handler := api.NewRouter(users, legacyHandler, api.TaskSurface{Tasks: tasks, Comments: comments, Labels: labels})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	slog.Info("server listening", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
