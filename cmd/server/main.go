// Command server runs the Pactline HTTP API.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/wolfhead/pactline"
	contract "github.com/wolfhead/pactline/api"
	"github.com/wolfhead/pactline/internal/access"
	"github.com/wolfhead/pactline/internal/api"
	apiv1 "github.com/wolfhead/pactline/internal/api/v1"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/integrations/devauth"
	"github.com/wolfhead/pactline/internal/integrations/lark"
	legacyapi "github.com/wolfhead/pactline/internal/legacy/api"
	legacystore "github.com/wolfhead/pactline/internal/legacy/store"
	"github.com/wolfhead/pactline/internal/logging"
	"github.com/wolfhead/pactline/internal/store"
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

	if err := db.Migrate(context.Background(), pactline.MigrationFS); err != nil {
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
	tokenService := access.NewService(
		store.NewAccessStore(db), identity.SystemClock{}, access.CryptoSecretGenerator{},
	)
	accessAuditStore := store.NewAccessAuditStore(db)
	idempotencyStore := store.NewIdempotencyStore(db)
	maintenanceContext, stopMaintenance := context.WithCancel(context.Background())
	defer stopMaintenance()
	go (application.Maintenance{Store: accessAuditStore}).Run(maintenanceContext)
	if cfg.AuthProvider == AuthProviderLark {
		cipher, cipherErr := identity.NewCredentialCipher(map[string][]byte{
			cfg.TokenEncryptionKeyID: cfg.TokenEncryptionKey,
		})
		if cipherErr != nil {
			slog.Error("configure credential encryption", "error", cipherErr)
			os.Exit(1)
		}
		larkClient, clientErr := lark.NewClient(lark.Config{
			AppID: cfg.LarkAppID, AppSecret: cfg.LarkAppSecret, TenantKey: cfg.LarkTenantKey,
			Cipher: cipher, EncryptionKeyID: cfg.TokenEncryptionKeyID,
			RedirectURI: cfg.LarkRedirectURI.String(),
		})
		if clientErr != nil {
			slog.Error("configure Lark client", "error", clientErr)
			os.Exit(1)
		}
		if configureErr := identityService.ConfigureLark(identity.LarkServiceConfig{
			Repository: identityStore, Authenticator: larkClient, Verifier: larkClient,
			Directory: larkClient, Notifier: larkClient, AppBaseURL: cfg.AppBaseURL.String(),
			TenantID: cfg.LarkTenantKey, RedirectURI: cfg.LarkRedirectURI.String(),
			BootstrapAdminEmail: cfg.BootstrapAdminEmail,
		}); configureErr != nil {
			slog.Error("configure Lark identity service", "error", configureErr)
			os.Exit(1)
		}
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

	v1Handler, err := apiv1.NewServer(&apiv1.Handler{
		Users: users,
		Tasks: &application.TaskService{
			Tasks: tasks, Comments: comments, Projects: projectService,
		},
		Labels:   &application.LabelService{Labels: labels},
		Projects: projectService,
	})
	if err != nil {
		slog.Error("configure OpenAPI v1 server", "error", err)
		os.Exit(1)
	}
	handler := api.NewRouter(legacyHandler, api.RouterOptions{
		Auth: api.AuthSurface{
			Sessions: identityService, Tokens: tokenService,
			AccessAudit: accessAuditStore,
			Idempotency: idempotencyStore,
			Development: developmentAuth, AppBaseURL: cfg.AppBaseURL,
			LarkEnabled:   cfg.AuthProvider == AuthProviderLark,
			SecureCookies: cfg.AppEnv != EnvironmentDevelopment && cfg.AppEnv != EnvironmentTest,
		},
		V1:      v1Handler,
		OpenAPI: apiv1.OpenAPIHandler(contract.OpenAPIDocument),
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
