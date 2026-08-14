package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var Version = "0.1.0-dev"

type App struct {
	stdin                                io.Reader
	stdout, stderr                       io.Writer
	server, token, clientKind, sessionID string
	jsonOutput, verbose                  bool
	idempotencyKey                       string
	client                               *client
}

func New(stdin io.Reader, stdout, stderr io.Writer) *cobra.Command {
	app := &App{stdin: stdin, stdout: stdout, stderr: stderr}
	root := &cobra.Command{
		Use: "pactline", SilenceUsage: true, SilenceErrors: true,
		Short: "Work with Pactline Tasks and Claims from any machine",
		Long: `Pactline is a stateless command-line client for people and Agents.

It always targets work explicitly: first a Task number, then the Claim ID
returned by the server. Client Session ID is audit provenance, never ownership.

Quick start:
  1. pactline config set --server https://pactline.example --token-stdin
  2. pactline doctor
  3. pactline task list
  4. pactline task show 142
  5. pactline task claim 142 --task-version 4
  6. pactline claim show <claim-id>

Use "pactline help workflow" for the full execution loop.`,
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			if command.Name() == "version" || command.Name() == "capabilities" || command.CommandPath() == "pactline help" ||
				(command.Parent() != nil && (command.Parent().Name() == "config" || command.Parent().Name() == "help")) {
				return nil
			}
			return app.initialize(command.Context())
		},
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVar(&app.server, "server", "", "Pactline server URL (or PACTLINE_SERVER)")
	root.PersistentFlags().StringVar(&app.clientKind, "client-kind", "", "non-secret caller kind (or PACTLINE_CLIENT_KIND)")
	root.PersistentFlags().StringVar(&app.sessionID, "session-id", "", "non-secret caller session provenance (or PACTLINE_SESSION_ID)")
	root.PersistentFlags().BoolVar(&app.jsonOutput, "json", false, "emit exactly one machine-readable JSON document")
	root.PersistentFlags().BoolVarP(&app.verbose, "verbose", "v", false, "write redacted HTTP diagnostics to stderr")
	root.PersistentFlags().StringVar(&app.idempotencyKey, "idempotency-key", "", "reuse this key for one mutation after an uncertain outcome")
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(app.versionCommand(), app.capabilitiesCommand(), app.configCommand(), app.doctorCommand(), app.taskCommand(), app.claimCommand())
	root.SetHelpCommand(app.helpCommand(root))
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return &APIError{Code: "USAGE", Message: err.Error()} })
	return root
}

func Execute(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	return ExecuteArgs(ctx, os.Args[1:], stdin, stdout, stderr)
}

func ExecuteArgs(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := New(stdin, stdout, stderr)
	root.SetContext(ctx)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		appErr := classifyError(err)
		jsonMode := false
		for _, argument := range args {
			jsonMode = jsonMode || argument == "--json"
		}
		if jsonMode {
			_ = json.NewEncoder(stdout).Encode(map[string]any{"ok": false, "error": appErr})
		} else {
			fmt.Fprintf(stderr, "Error [%s]: %s\n", appErr.Code, appErr.Message)
			if appErr.Hint != "" {
				fmt.Fprintf(stderr, "Hint: %s\n", appErr.Hint)
			}
			if appErr.RequestID != "" {
				fmt.Fprintf(stderr, "Request ID: %s\n", appErr.RequestID)
			}
			if appErr.Key != "" {
				fmt.Fprintf(stderr, "Idempotency key: %s\n", appErr.Key)
			}
		}
		return exitCode(appErr)
	}
	return 0
}

func (a *App) initialize(context.Context) error {
	config, _, err := loadConfig()
	if err != nil {
		return &APIError{Code: "CONFIG_ERROR", Message: err.Error()}
	}
	if a.server == "" {
		a.server = firstNonEmpty(os.Getenv("PACTLINE_SERVER"), config.Server)
	}
	a.token = firstNonEmpty(os.Getenv("PACTLINE_TOKEN"), config.Token)
	if a.clientKind == "" {
		a.clientKind = firstNonEmpty(os.Getenv("PACTLINE_CLIENT_KIND"), config.ClientKind, "pactline-cli")
	}
	if a.sessionID == "" {
		a.sessionID = firstNonEmpty(os.Getenv("PACTLINE_SESSION_ID"), os.Getenv("CODEX_THREAD_ID"))
	}
	if value := strings.TrimSpace(os.Getenv("PACTLINE_VERBOSE")); !a.verbose && (value == "1" || strings.EqualFold(value, "true")) {
		a.verbose = true
	}
	if a.server == "" || a.token == "" {
		return &APIError{Code: "CONFIG_ERROR", Message: "server and Token are required", Hint: "Run pactline config set or set PACTLINE_SERVER and PACTLINE_TOKEN."}
	}
	parsed, err := url.Parse(a.server)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return &APIError{Code: "CONFIG_ERROR", Message: "server must be an absolute HTTP(S) URL"}
	}
	a.server = strings.TrimRight(a.server, "/")
	a.client = &client{
		server: a.server, token: a.token, clientKind: a.clientKind, sessionID: a.sessionID,
		httpClient: &http.Client{Timeout: 30 * time.Second}, verbose: a.debugf,
	}
	a.debugf("configuration server=%s client_kind=%s session_id=%s token=configured", a.server, a.clientKind, a.sessionID)
	return nil
}

func (a *App) requireMutationProvenance() error {
	if a.sessionID == "" {
		return &APIError{Code: "CONFIG_ERROR", Message: "Session ID is required for Claim mutations", Hint: "Set PACTLINE_SESSION_ID or CODEX_THREAD_ID."}
	}
	return nil
}

func (a *App) debugf(format string, values ...any) {
	if a.verbose {
		fmt.Fprintf(a.stderr, "[verbose] "+format+"\n", values...)
	}
}

func (a *App) output(data any, human func(io.Writer)) error {
	if a.jsonOutput {
		envelope := map[string]any{"ok": true, "data": data}
		if a.client != nil && !a.client.lastMeta.empty() {
			envelope["meta"] = a.client.lastMeta
		}
		return json.NewEncoder(a.stdout).Encode(envelope)
	}
	human(a.stdout)
	return nil
}

func (a *App) capabilitiesCommand() *cobra.Command {
	features := []string{
		"bounded_work_packets",
		"claim_progress",
		"claim_release",
		"execution_claims",
		"execution_completion",
		"execution_verification",
		"gitlab_merge_request_links",
		"repeatable_submission",
		"resolution_request",
		"success_metadata",
	}
	return &cobra.Command{
		Use: "capabilities", Short: "Print the offline machine-integration contract",
		Long: "Reports features implemented by this CLI binary. It never reads configuration or contacts the server.",
		Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			data := map[string]any{"protocol": 1, "cli_version": Version, "features": features}
			return a.output(data, func(w io.Writer) {
				fmt.Fprintf(w, "Protocol: 1\nCLI version: %s\nFeatures:\n", Version)
				for _, feature := range features {
					fmt.Fprintf(w, "  - %s\n", feature)
				}
			})
		},
	}
}

func (a *App) versionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print CLI version", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		return a.output(map[string]string{"version": Version}, func(w io.Writer) { fmt.Fprintln(w, Version) })
	}}
}

func (a *App) configCommand() *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "Manage the single local profile"}
	var server, kind string
	var tokenStdin bool
	set := &cobra.Command{
		Use: "set", Short: "Atomically store server and Token with mode 0600",
		Long:    "Read a Token only from stdin so it never appears in shell history or process listings.",
		Example: `printf '%s' "$TOKEN" | pactline config set --server https://pactline.example --token-stdin`,
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if !tokenStdin || strings.TrimSpace(server) == "" {
				return &APIError{Code: "USAGE", Message: "--server and --token-stdin are required"}
			}
			token, err := readSecret(a.stdin)
			if err != nil {
				return &APIError{Code: "CONFIG_ERROR", Message: err.Error()}
			}
			if kind == "" {
				kind = "pactline-cli"
			}
			path, err := saveConfig(Config{Server: strings.TrimRight(server, "/"), Token: token, ClientKind: kind})
			if err != nil {
				return &APIError{Code: "CONFIG_ERROR", Message: err.Error()}
			}
			return a.output(map[string]any{"path": path, "server": server, "client_kind": kind, "token": "configured"}, func(w io.Writer) {
				fmt.Fprintf(w, "Saved: %s\nServer: %s\nClient kind: %s\nToken: configured\n", path, server, kind)
			})
		},
	}
	set.Flags().StringVar(&server, "server", "", "absolute Pactline server URL")
	set.Flags().StringVar(&kind, "client-kind", "pactline-cli", "default non-secret client kind")
	set.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read the Token from stdin")
	show := &cobra.Command{Use: "show", Short: "Show non-secret configuration", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		config, path, err := loadConfig()
		if err != nil {
			return &APIError{Code: "CONFIG_ERROR", Message: err.Error()}
		}
		data := map[string]any{"path": path, "server": config.Server, "client_kind": config.ClientKind, "token": configured(config.Token)}
		return a.output(data, func(w io.Writer) {
			fmt.Fprintf(w, "Path: %s\nServer: %s\nClient kind: %s\nToken: %s\n", path, config.Server, config.ClientKind, configured(config.Token))
		})
	}}
	command.AddCommand(set, show)
	return command
}

func (a *App) doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use: "doctor", Short: "Validate configuration and authentication without mutating work",
		Long: "Checks the effective server, Token, client provenance, and /api/v1/me. It never prints the Token.",
		Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
			body, _, err := a.client.request(command.Context(), http.MethodGet, "/api/v1/me", nil, 0, "", false)
			if err != nil {
				return err
			}
			var principal map[string]any
			if err := json.Unmarshal(body, &principal); err != nil {
				return err
			}
			data := map[string]any{"server": a.server, "client_kind": a.clientKind, "session_id": a.sessionID, "token": "configured", "principal": principal}
			return a.output(data, func(w io.Writer) {
				fmt.Fprintf(w, "Server: %s\nToken: configured\nClient kind: %s\nSession ID: %s\nAuthentication: OK\n", a.server, a.clientKind, a.sessionID)
			})
		},
	}
}

func requiredPositive(flag string, value int64) error {
	if value < 1 {
		return &APIError{Code: "USAGE", Message: "--" + flag + " must be a positive integer"}
	}
	return nil
}

func parsePositive(value, label string) (int64, error) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 1 {
		return 0, &APIError{Code: "USAGE", Message: label + " must be a positive integer"}
	}
	return number, nil
}

func classifyError(err error) *APIError {
	var apiError *APIError
	if errors.As(err, &apiError) {
		return apiError
	}
	if strings.HasPrefix(err.Error(), "unknown command ") ||
		strings.HasPrefix(err.Error(), "accepts ") ||
		strings.HasPrefix(err.Error(), "requires ") {
		return &APIError{Code: "USAGE", Message: err.Error()}
	}
	return &APIError{Code: "CLIENT_ERROR", Message: err.Error()}
}

func exitCode(err *APIError) int {
	if err.Code == "USAGE" || err.Code == "CONFIG_ERROR" || err.Code == "REVIEW_NOT_SUPPORTED" {
		return 2
	}
	if err.Status == 401 || err.Status == 403 {
		return 3
	}
	if err.Status == 409 || err.Status == 412 || err.Status == 422 || err.Status == 400 {
		return 4
	}
	return 5
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func configured(value string) string {
	if value == "" {
		return "missing"
	}
	return "configured"
}
