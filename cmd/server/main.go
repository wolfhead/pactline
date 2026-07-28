// Command server runs the Bounty Board HTTP API.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"bountyboard"
	"bountyboard/internal/api"
	"bountyboard/internal/application"
	"bountyboard/internal/identity"
	"bountyboard/internal/integrations/devauth"
	legacyapi "bountyboard/internal/legacy/api"
	legacystore "bountyboard/internal/legacy/store"
	"bountyboard/internal/logging"
	"bountyboard/internal/store"
)

func main() {
	logging.Setup(os.Getenv("LOG_LEVEL"))

	cfg, err := LoadConfig()
	if err != nil {
		slog.Error("invalid server configuration", "error", err)
		os.Exit(1)
	}

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
	projects := store.NewProjectStore(db)
	milestones := store.NewMilestoneStore(db)
	acceptance := store.NewAcceptanceStore(db)
	projectService := &application.ProjectService{
		Projects: projects, Milestones: milestones, Acceptance: acceptance, Tasks: tasks,
	}
	identityStore := store.NewIdentityStore(db)
	identityService, err := identity.NewService(
		identityStore, users, cfg.SessionSecret, identity.SystemClock{}, identity.CryptoSecretGenerator{},
	)
	if err != nil {
		slog.Error("configure application sessions", "error", err)
		os.Exit(1)
	}
	var developmentAuth *devauth.Provider
	if cfg.AuthProvider == AuthProviderDevelopment {
		developmentAuth = devauth.New(users, identityService)
	}

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

	taskSurface := &api.TaskSurface{
		Tasks: tasks, Comments: comments, Labels: labels, Projects: projects,
		Milestones: milestones, Acceptance: acceptance, ProjectService: projectService,
	}
	handler := api.NewRouter(users, legacyHandler, api.RouterOptions{
		Auth: api.AuthSurface{
			Sessions: identityService, Development: developmentAuth, AppBaseURL: cfg.AppBaseURL,
			SecureCookies: cfg.AppEnv != EnvironmentDevelopment && cfg.AppEnv != EnvironmentTest,
		},
		Tasks: taskSurface,
	})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	slog.Info("server listening", "addr", addr, "app_env", cfg.AppEnv, "auth_provider", cfg.AuthProvider)
	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
