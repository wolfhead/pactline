package main

import (
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func validProductionConfig() Config {
	base, _ := url.Parse("https://tasks.example.test")
	redirect, _ := url.Parse("https://tasks.example.test/api/auth/lark/callback")
	return Config{
		AppEnv: EnvironmentProduction, AuthProvider: AuthProviderLark,
		AppBaseURL: base, SessionSecret: make([]byte, 32),
		TokenEncryptionKey: make([]byte, 32), TokenEncryptionKeyID: "key-1",
		LarkAppID: "app-id", LarkAppSecret: "secret", LarkTenantKey: "tenant",
		LarkRedirectURI: redirect, BootstrapAdminEmail: "admin@example.test",
	}
}

func TestProductionRejectsDevelopmentAuth(t *testing.T) {
	cfg := validProductionConfig()
	cfg.AuthProvider = AuthProviderDevelopment
	require.ErrorContains(t, cfg.Validate(), "development authentication is not allowed in production")
}

func TestProductionRequiresHTTPSAndSecrets(t *testing.T) {
	cfg := validProductionConfig()
	cfg.AppBaseURL, _ = url.Parse("http://tasks.example.test")
	require.ErrorContains(t, cfg.Validate(), "APP_BASE_URL must use HTTPS")
	cfg = validProductionConfig()
	cfg.SessionSecret = []byte("short")
	require.ErrorContains(t, cfg.Validate(), "SESSION_SECRET")
	cfg = validProductionConfig()
	cfg.TokenEncryptionKey = []byte("short")
	require.ErrorContains(t, cfg.Validate(), "exactly 32 bytes")
}

func TestLarkConfigurationRequiresValuesAndMatchingOrigin(t *testing.T) {
	cfg := validProductionConfig()
	cfg.BootstrapAdminEmail = ""
	require.ErrorContains(t, cfg.Validate(), "BOOTSTRAP_ADMIN_EMAIL")
	cfg = validProductionConfig()
	cfg.LarkRedirectURI, _ = url.Parse("https://other.example.test/callback")
	require.ErrorContains(t, cfg.Validate(), "same origin")
	cfg = validProductionConfig()
	cfg.LarkAppSecret = ""
	require.ErrorContains(t, cfg.Validate(), "LARK_APP_ID")
}

func TestUnsupportedProviderIsRejected(t *testing.T) {
	cfg := validProductionConfig()
	cfg.AuthProvider = "unknown"
	require.ErrorContains(t, cfg.Validate(), "unsupported AUTH_PROVIDER")
}

func TestLoadConfigDecodesEncryptionKey(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_PROVIDER", "lark")
	t.Setenv("APP_BASE_URL", "https://tasks.example.test")
	t.Setenv("SESSION_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY_ID", "key-1")
	t.Setenv("LARK_APP_ID", "app-id")
	t.Setenv("LARK_APP_SECRET", "secret")
	t.Setenv("LARK_TENANT_KEY", "tenant")
	t.Setenv("LARK_REDIRECT_URI", "https://tasks.example.test/api/auth/lark/callback")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "admin@example.test")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Len(t, cfg.TokenEncryptionKey, 32)

	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 31)))
	_, err = LoadConfig()
	require.ErrorContains(t, err, "exactly 32 bytes")
}

func TestDevelopmentAndTestRequireDecoded32ByteSessionSecret(t *testing.T) {
	base, err := url.Parse("http://localhost:5173")
	require.NoError(t, err)
	for _, environment := range []string{EnvironmentDevelopment, EnvironmentTest} {
		cfg := Config{
			AppEnv: environment, AuthProvider: AuthProviderDevelopment,
			AppBaseURL: base, SessionSecret: make([]byte, 31),
		}
		require.ErrorContains(t, cfg.Validate(), "SESSION_SECRET must decode to exactly 32 bytes")
	}

	t.Setenv("APP_ENV", EnvironmentDevelopment)
	t.Setenv("AUTH_PROVIDER", AuthProviderDevelopment)
	t.Setenv("APP_BASE_URL", "http://localhost:5173")
	t.Setenv("SESSION_SECRET", "not-base64!")
	_, err = LoadConfig()
	require.ErrorContains(t, err, "SESSION_SECRET must be base64 encoded")
}

func TestReadConfigurationValueFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-secret")
	require.NoError(t, os.WriteFile(path, []byte("secret-value\n"), 0o600))
	t.Setenv("SESSION_SECRET_FILE", path)

	value, err := readConfigurationValue("SESSION_SECRET")

	require.NoError(t, err)
	require.Equal(t, "secret-value", value)
}

func TestReadConfigurationValueRejectsAmbiguousSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-secret")
	require.NoError(t, os.WriteFile(path, []byte("file-value"), 0o600))
	t.Setenv("SESSION_SECRET", "environment-value")
	t.Setenv("SESSION_SECRET_FILE", path)

	_, err := readConfigurationValue("SESSION_SECRET")

	require.ErrorContains(t, err, "SESSION_SECRET and SESSION_SECRET_FILE cannot both be set")
}

func TestReadConfigurationValueReportsUnreadableFile(t *testing.T) {
	t.Setenv("SESSION_SECRET_FILE", filepath.Join(t.TempDir(), "missing"))

	_, err := readConfigurationValue("SESSION_SECRET")

	require.ErrorContains(t, err, "read SESSION_SECRET_FILE")
}

func TestLoadConfigReadsProductionSecretsFromFiles(t *testing.T) {
	secretDir := t.TempDir()
	writeSecret := func(name, value string) string {
		path := filepath.Join(secretDir, name)
		require.NoError(t, os.WriteFile(path, []byte(value+"\n"), 0o600))
		return path
	}
	t.Setenv("APP_ENV", "production")
	t.Setenv("AUTH_PROVIDER", "lark")
	t.Setenv("APP_BASE_URL", "https://tasks.example.test")
	t.Setenv("SESSION_SECRET_FILE", writeSecret(
		"session_secret",
		base64.StdEncoding.EncodeToString(make([]byte, 32)),
	))
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY_FILE", writeSecret(
		"oauth_token_encryption_key",
		base64.StdEncoding.EncodeToString(make([]byte, 32)),
	))
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY_ID", "key-1")
	t.Setenv("LARK_APP_ID", "app-id")
	t.Setenv("LARK_APP_SECRET_FILE", writeSecret("lark_app_secret", "app-secret"))
	t.Setenv("LARK_TENANT_KEY", "tenant")
	t.Setenv("LARK_REDIRECT_URI", "https://tasks.example.test/api/auth/lark/callback")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "admin@example.test")

	cfg, err := LoadConfig()

	require.NoError(t, err)
	require.Len(t, cfg.SessionSecret, 32)
	require.Len(t, cfg.TokenEncryptionKey, 32)
	require.Equal(t, "app-secret", cfg.LarkAppSecret)
}

func TestAgentConfigRequiresLarkAndDedicatedSecrets(t *testing.T) {
	baseURL, err := url.Parse("https://tasks.example.test")
	require.NoError(t, err)
	redirectURL, err := url.Parse("https://tasks.example.test/api/auth/lark/callback")
	require.NoError(t, err)
	config := Config{
		AppEnv: EnvironmentProduction, AuthProvider: AuthProviderLark,
		AppBaseURL: baseURL, SessionSecret: make([]byte, 32),
		TokenEncryptionKey: make([]byte, 32), TokenEncryptionKeyID: "oauth-key",
		LarkAppID: "app", LarkAppSecret: "secret", LarkTenantKey: "tenant",
		LarkRedirectURI: redirectURL, BootstrapAdminEmail: "admin@example.test",
		AgentEnabled: true, DeepSeekAPIKey: "test-only-key",
		AgentDelegationSigningKey: make([]byte, 32), AgentDelegationSigningKeyID: "delegate-key",
		AgentCheckpointEncryptionKey: make([]byte, 32), AgentCheckpointEncryptionKeyID: "checkpoint-key",
		AgentWorkerConcurrency: 1, AgentTenantTimezone: "Asia/Shanghai",
		LarkEventVerificationToken: "verification-token",
		LarkEventEncryptKey:        "encrypt-key", LarkBotOpenID: "ou_bot",
	}
	require.NoError(t, config.Validate())

	config.DeepSeekAPIKey = ""
	require.EqualError(t, config.Validate(), "DEEPSEEK_API_KEY is required when AGENT_ENABLED=true")
	config.DeepSeekAPIKey = "test-only-key"
	config.AuthProvider = AuthProviderDevelopment
	require.EqualError(t, config.Validate(), "development authentication is not allowed in production")
}
