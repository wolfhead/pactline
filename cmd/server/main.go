// Command server runs the Pactline HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wolfhead/pactline"
	contract "github.com/wolfhead/pactline/api"
	"github.com/wolfhead/pactline/internal/access"
	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/agent/channel"
	"github.com/wolfhead/pactline/internal/agent/ingress"
	agentopenapi "github.com/wolfhead/pactline/internal/agent/openapi"
	"github.com/wolfhead/pactline/internal/agent/reply"
	agentruntime "github.com/wolfhead/pactline/internal/agent/runtime"
	"github.com/wolfhead/pactline/internal/api"
	apiv1 "github.com/wolfhead/pactline/internal/api/v1"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/identity"
	"github.com/wolfhead/pactline/internal/integrations/deepseek"
	"github.com/wolfhead/pactline/internal/integrations/devauth"
	"github.com/wolfhead/pactline/internal/integrations/lark"
	legacyapi "github.com/wolfhead/pactline/internal/legacy/api"
	legacystore "github.com/wolfhead/pactline/internal/legacy/store"
	"github.com/wolfhead/pactline/internal/logging"
	"github.com/wolfhead/pactline/internal/store"

	"github.com/cloudwego/eino/components/model"
)

func main() {
	logging.Setup(os.Getenv("LOG_LEVEL"))

	cfg, err := LoadConfig()
	if err != nil {
		slog.Error("invalid server configuration", "error", err)
		os.Exit(1)
	}
	dsn, err := readConfigurationValue("DATABASE_URL")
	if err != nil {
		slog.Error("load database configuration", "error", err)
		os.Exit(1)
	}
	if dsn == "" {
		slog.Error("DATABASE_URL or DATABASE_URL_FILE is required")
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
	agentStore := store.NewAgentStore(db)
	maintenanceContext, stopMaintenance := context.WithCancel(context.Background())
	defer stopMaintenance()
	go (application.Maintenance{Store: accessAuditStore}).Run(maintenanceContext)
	var larkClient *lark.Client
	if cfg.AuthProvider == AuthProviderLark {
		cipher, cipherErr := identity.NewCredentialCipher(map[string][]byte{
			cfg.TokenEncryptionKeyID: cfg.TokenEncryptionKey,
		})
		if cipherErr != nil {
			slog.Error("configure credential encryption", "error", cipherErr)
			os.Exit(1)
		}
		configuredLarkClient, clientErr := lark.NewClient(lark.Config{
			AppID: cfg.LarkAppID, AppSecret: cfg.LarkAppSecret,
			Cipher: cipher, EncryptionKeyID: cfg.TokenEncryptionKeyID,
			RedirectURI: cfg.LarkRedirectURI.String(),
		})
		if clientErr != nil {
			slog.Error("configure Lark client", "error", clientErr)
			os.Exit(1)
		}
		larkClient = configuredLarkClient
		initializationContext, cancelInitialization := context.WithTimeout(
			context.Background(),
			15*time.Second,
		)
		tenantKey, initializationErr := larkClient.InitializeTenant(initializationContext)
		cancelInitialization()
		if initializationErr != nil {
			slog.Error("initialize Lark tenant",
				"error_category", "tenant_initialization",
				"error", initializationErr)
			os.Exit(1)
		}
		slog.Info("Lark tenant initialized")
		if configureErr := identityService.ConfigureLark(identity.LarkServiceConfig{
			Repository: identityStore, Authenticator: larkClient, Verifier: larkClient,
			Directory: larkClient, Notifier: larkClient, AppBaseURL: cfg.AppBaseURL.String(),
			TenantID: tenantKey, RedirectURI: cfg.LarkRedirectURI.String(),
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
	var (
		delegateService *access.DelegateService
		agentConnection *lark.LongConnection
		inputCipher     *pactagent.InputCipher
		checkpointStore *pactagent.EncryptedCheckpointStore
		agentTimezone   *time.Location
	)
	if cfg.AgentEnabled {
		delegateService, err = access.NewDelegateService(access.DelegateConfig{
			ActiveKeyID: cfg.AgentDelegationSigningKeyID,
			SigningKeys: map[string][]byte{
				cfg.AgentDelegationSigningKeyID: cfg.AgentDelegationSigningKey,
			},
		}, agentStore, users, identity.SystemClock{})
		if err != nil {
			slog.Error("configure Agent delegation", "error", err)
			os.Exit(1)
		}
		inputCipher, err = pactagent.NewInputCipher(
			cfg.AgentCheckpointEncryptionKeyID,
			cfg.AgentCheckpointEncryptionKey,
		)
		if err != nil {
			slog.Error("configure Agent input encryption", "error", err)
			os.Exit(1)
		}
		checkpointStore, err = pactagent.NewEncryptedCheckpointStore(
			agentStore,
			cfg.AgentCheckpointEncryptionKeyID,
			cfg.AgentCheckpointEncryptionKey,
			cfg.DeepSeekModel,
			time.Now,
		)
		if err != nil {
			slog.Error("configure Agent checkpoint encryption", "error", err)
			os.Exit(1)
		}
		agentTimezone, err = time.LoadLocation(cfg.AgentTenantTimezone)
		if err != nil {
			slog.Error("configure Agent tenant timezone", "error", err)
			os.Exit(1)
		}
		agentIngress, ingressErr := ingress.New(ingress.Config{
			Identities:    identityStore,
			Runs:          agentStore,
			Inputs:        inputCipher,
			Model:         cfg.DeepSeekModel,
			PromptVersion: agentruntime.PromptVersion,
			Now:           time.Now,
		})
		if ingressErr != nil {
			slog.Error("configure Agent channel ingress", "error", ingressErr)
			os.Exit(1)
		}
		initializationContext, cancelInitialization := context.WithTimeout(
			context.Background(),
			15*time.Second,
		)
		initializationErr := larkClient.InitializeAgentChannel(initializationContext)
		cancelInitialization()
		if initializationErr != nil {
			slog.Error("initialize Lark Agent channel",
				"error_category", "bot_initialization",
				"error", initializationErr)
			os.Exit(1)
		}
		agentConnection, err = lark.NewLongConnection(
			cfg.LarkAppID,
			cfg.LarkAppSecret,
			larkClient,
			agentIngress,
		)
		if err != nil {
			slog.Error("configure Lark Agent long connection", "error", err)
			os.Exit(1)
		}
		slog.Info("Lark Agent channel initialized", "transport", "websocket")
	}
	applicationHandler := api.NewRouter(legacyHandler, api.RouterOptions{
		Auth: api.AuthSurface{
			Sessions: identityService, Tokens: tokenService,
			Delegates:   delegateService,
			AccessAudit: accessAuditStore,
			Idempotency: idempotencyStore,
			Development: developmentAuth, AppBaseURL: cfg.AppBaseURL,
			LarkEnabled:   cfg.AuthProvider == AuthProviderLark,
			SecureCookies: cfg.AppEnv != EnvironmentDevelopment && cfg.AppEnv != EnvironmentTest,
		},
		V1:          v1Handler,
		OpenAPI:     apiv1.OpenAPIHandler(contract.OpenAPIDocument),
		AgentStatus: agentConnection,
	})
	var agentWorker *agentruntime.Worker
	if cfg.AgentEnabled {
		clientFactory, factoryErr := agentopenapi.NewFactory(delegateService, applicationHandler)
		if factoryErr != nil {
			slog.Error("configure Agent OpenAPI client", "error", factoryErr)
			os.Exit(1)
		}
		agentWorker, err = agentruntime.New(agentruntime.Config{
			Repository: agentStore,
			Channels: map[string]channel.ChannelAdapter{
				"lark": larkClient,
			},
			OpenAPI:         clientFactory,
			InputCipher:     inputCipher,
			CheckpointStore: checkpointStore,
			Renderer:        reply.Renderer{AppBaseURL: cfg.AppBaseURL},
			WorkerID:        "pactline",
			Concurrency:     cfg.AgentWorkerConcurrency,
			Timezone:        agentTimezone,
			Model: func(ctx context.Context, run pactagent.Run) (model.ToolCallingChatModel, error) {
				return deepseek.NewChatModel(ctx, deepseek.Config{
					APIKey:  cfg.DeepSeekAPIKey,
					BaseURL: cfg.DeepSeekBaseURL,
					Model:   run.Model,
				})
			},
		})
		if err != nil {
			slog.Error("configure Agent worker", "error", err)
			os.Exit(1)
		}
	}
	handler := withOperationalEndpoints(applicationHandler, db.Pool)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	runContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if agentWorker != nil {
		go agentConnection.Run(runContext)
		go agentWorker.Run(runContext)
		slog.Info("Agent worker started",
			"concurrency", cfg.AgentWorkerConcurrency,
			"model", cfg.DeepSeekModel,
			"prompt_version", agentruntime.PromptVersion)
	}
	shutdownComplete := make(chan struct{})
	go func() {
		<-runContext.Done()
		slog.Info("server shutdown started")
		shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownContext); shutdownErr != nil {
			slog.Error("server graceful shutdown failed", "error", shutdownErr)
		}
		close(shutdownComplete)
	}()

	slog.Info("server listening", "addr", addr, "app_env", cfg.AppEnv, "auth_provider", cfg.AuthProvider)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
	if runContext.Err() != nil {
		<-shutdownComplete
		slog.Info("server shutdown complete")
	}
}
