package main

import (
	"encoding/base64"
	"net/url"
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
	t.Setenv("SESSION_SECRET", "01234567890123456789012345678901")
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
