package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvironmentDevelopment  = "development"
	EnvironmentTest         = "test"
	EnvironmentProduction   = "production"
	AuthProviderDevelopment = "development"
	AuthProviderLark        = "lark"
)

type Config struct {
	AppEnv                         string
	AuthProvider                   string
	AppBaseURL                     *url.URL
	SessionSecret                  []byte
	TokenEncryptionKey             []byte
	TokenEncryptionKeyID           string
	LarkAppID                      string
	LarkAppSecret                  string
	LarkTenantKey                  string
	LarkRedirectURI                *url.URL
	BootstrapAdminEmail            string
	AgentEnabled                   bool
	DeepSeekAPIKey                 string
	DeepSeekBaseURL                string
	DeepSeekModel                  string
	AgentDelegationSigningKey      []byte
	AgentDelegationSigningKeyID    string
	AgentCheckpointEncryptionKey   []byte
	AgentCheckpointEncryptionKeyID string
	AgentWorkerConcurrency         int
	AgentTenantTimezone            string
}

func LoadConfig() (Config, error) {
	sessionSecret, err := readConfigurationValue("SESSION_SECRET")
	if err != nil {
		return Config{}, err
	}
	tokenEncryptionKey, err := readConfigurationValue("OAUTH_TOKEN_ENCRYPTION_KEY")
	if err != nil {
		return Config{}, err
	}
	larkAppSecret, err := readConfigurationValue("LARK_APP_SECRET")
	if err != nil {
		return Config{}, err
	}
	deepSeekAPIKey, err := readConfigurationValue("DEEPSEEK_API_KEY")
	if err != nil {
		return Config{}, err
	}
	delegationSigningKey, err := readConfigurationValue("AGENT_DELEGATION_SIGNING_KEY")
	if err != nil {
		return Config{}, err
	}
	checkpointEncryptionKey, err := readConfigurationValue("AGENT_CHECKPOINT_ENCRYPTION_KEY")
	if err != nil {
		return Config{}, err
	}
	agentEnabled, err := parseOptionalBool("AGENT_ENABLED", os.Getenv("AGENT_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	agentConcurrency, err := parseOptionalInt(
		"AGENT_WORKER_CONCURRENCY", os.Getenv("AGENT_WORKER_CONCURRENCY"), 1,
	)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		AppEnv:                         strings.TrimSpace(os.Getenv("APP_ENV")),
		AuthProvider:                   strings.TrimSpace(os.Getenv("AUTH_PROVIDER")),
		TokenEncryptionKeyID:           strings.TrimSpace(os.Getenv("OAUTH_TOKEN_ENCRYPTION_KEY_ID")),
		LarkAppID:                      strings.TrimSpace(os.Getenv("LARK_APP_ID")),
		LarkAppSecret:                  larkAppSecret,
		LarkTenantKey:                  strings.TrimSpace(os.Getenv("LARK_TENANT_KEY")),
		BootstrapAdminEmail:            strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")),
		AgentEnabled:                   agentEnabled,
		DeepSeekAPIKey:                 strings.TrimSpace(deepSeekAPIKey),
		DeepSeekBaseURL:                strings.TrimSpace(os.Getenv("DEEPSEEK_BASE_URL")),
		DeepSeekModel:                  strings.TrimSpace(os.Getenv("DEEPSEEK_MODEL")),
		AgentDelegationSigningKeyID:    strings.TrimSpace(os.Getenv("AGENT_DELEGATION_SIGNING_KEY_ID")),
		AgentCheckpointEncryptionKeyID: strings.TrimSpace(os.Getenv("AGENT_CHECKPOINT_ENCRYPTION_KEY_ID")),
		AgentWorkerConcurrency:         agentConcurrency,
		AgentTenantTimezone:            strings.TrimSpace(os.Getenv("AGENT_TENANT_TIMEZONE")),
	}
	if cfg.DeepSeekModel == "" {
		cfg.DeepSeekModel = "deepseek-v4-pro"
	}
	if cfg.AgentTenantTimezone == "" {
		cfg.AgentTenantTimezone = "Asia/Shanghai"
	}
	cfg.SessionSecret, err = decodeSecret("SESSION_SECRET", sessionSecret)
	if err != nil {
		return Config{}, err
	}
	cfg.AppBaseURL, err = parseConfiguredURL("APP_BASE_URL", os.Getenv("APP_BASE_URL"))
	if err != nil {
		return Config{}, err
	}
	cfg.LarkRedirectURI, err = parseOptionalURL("LARK_REDIRECT_URI", os.Getenv("LARK_REDIRECT_URI"))
	if err != nil {
		return Config{}, err
	}
	if encoded := tokenEncryptionKey; encoded != "" {
		cfg.TokenEncryptionKey, err = decodeEncryptionKey(encoded)
		if err != nil {
			return Config{}, err
		}
	}
	if encoded := delegationSigningKey; encoded != "" {
		cfg.AgentDelegationSigningKey, err = decodeNamedEncryptionKey(
			"AGENT_DELEGATION_SIGNING_KEY", encoded,
		)
		if err != nil {
			return Config{}, err
		}
	}
	if encoded := checkpointEncryptionKey; encoded != "" {
		cfg.AgentCheckpointEncryptionKey, err = decodeNamedEncryptionKey(
			"AGENT_CHECKPOINT_ENCRYPTION_KEY", encoded,
		)
		if err != nil {
			return Config{}, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readConfigurationValue(name string) (string, error) {
	value := os.Getenv(name)
	fileName := name + "_FILE"
	path := strings.TrimSpace(os.Getenv(fileName))
	if value != "" && path != "" {
		return "", fmt.Errorf("%s and %s cannot both be set", name, fileName)
	}
	if path == "" {
		return value, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fileName, err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

func (c Config) Validate() error {
	switch c.AppEnv {
	case EnvironmentDevelopment, EnvironmentTest, EnvironmentProduction:
	default:
		return fmt.Errorf("unsupported APP_ENV %q", c.AppEnv)
	}
	switch c.AuthProvider {
	case AuthProviderDevelopment, AuthProviderLark:
	default:
		return fmt.Errorf("unsupported AUTH_PROVIDER %q", c.AuthProvider)
	}
	if c.AppBaseURL == nil || c.AppBaseURL.Scheme == "" || c.AppBaseURL.Host == "" {
		return errors.New("APP_BASE_URL must be an absolute URL")
	}
	if len(c.SessionSecret) != 32 {
		return errors.New("SESSION_SECRET must decode to exactly 32 bytes")
	}
	if c.AppEnv == EnvironmentProduction {
		if c.AuthProvider == AuthProviderDevelopment {
			return errors.New("development authentication is not allowed in production")
		}
		if c.AppBaseURL.Scheme != "https" {
			return errors.New("APP_BASE_URL must use HTTPS in production")
		}
	}
	if c.AuthProvider == AuthProviderLark {
		if len(c.TokenEncryptionKey) != 32 {
			return errors.New("OAUTH_TOKEN_ENCRYPTION_KEY must decode to exactly 32 bytes")
		}
		if c.TokenEncryptionKeyID == "" {
			return errors.New("OAUTH_TOKEN_ENCRYPTION_KEY_ID is required for Lark")
		}
		if c.LarkAppID == "" || c.LarkAppSecret == "" || c.LarkTenantKey == "" {
			return errors.New("LARK_APP_ID, LARK_APP_SECRET, and LARK_TENANT_KEY are required for Lark")
		}
		if c.LarkRedirectURI == nil || c.LarkRedirectURI.Scheme == "" || c.LarkRedirectURI.Host == "" {
			return errors.New("LARK_REDIRECT_URI must be an absolute URL for Lark")
		}
		if c.BootstrapAdminEmail == "" {
			return errors.New("BOOTSTRAP_ADMIN_EMAIL is required for Lark")
		}
		if !sameOrigin(c.AppBaseURL, c.LarkRedirectURI) {
			return errors.New("LARK_REDIRECT_URI and APP_BASE_URL must have the same origin")
		}
		if c.AppEnv == EnvironmentProduction && c.LarkRedirectURI.Scheme != "https" {
			return errors.New("LARK_REDIRECT_URI must use HTTPS in production")
		}
	}
	if c.AgentEnabled {
		if c.AuthProvider != AuthProviderLark {
			return errors.New("AGENT_ENABLED requires AUTH_PROVIDER=lark")
		}
		if c.DeepSeekAPIKey == "" {
			return errors.New("DEEPSEEK_API_KEY is required when AGENT_ENABLED=true")
		}
		if len(c.AgentDelegationSigningKey) != 32 ||
			c.AgentDelegationSigningKeyID == "" {
			return errors.New("AGENT_DELEGATION_SIGNING_KEY and AGENT_DELEGATION_SIGNING_KEY_ID are required")
		}
		if len(c.AgentCheckpointEncryptionKey) != 32 ||
			c.AgentCheckpointEncryptionKeyID == "" {
			return errors.New("AGENT_CHECKPOINT_ENCRYPTION_KEY and AGENT_CHECKPOINT_ENCRYPTION_KEY_ID are required")
		}
		if c.AgentWorkerConcurrency < 1 || c.AgentWorkerConcurrency > 8 {
			return errors.New("AGENT_WORKER_CONCURRENCY must be between 1 and 8")
		}
		if _, err := time.LoadLocation(c.AgentTenantTimezone); err != nil {
			return fmt.Errorf("AGENT_TENANT_TIMEZONE is invalid: %w", err)
		}
	}
	return nil
}

func decodeEncryptionKey(encoded string) ([]byte, error) {
	key, err := decodeSecret("OAUTH_TOKEN_ENCRYPTION_KEY", encoded)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func decodeNamedEncryptionKey(name, encoded string) ([]byte, error) {
	return decodeSecret(name, encoded)
}

func parseOptionalBool(name, raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func parseOptionalInt(name, raw string, defaultValue int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return value, nil
}

func decodeSecret(name, encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, fmt.Errorf("%s must be base64 encoded", name)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s must decode to exactly 32 bytes", name)
	}
	return key, nil
}

func parseConfiguredURL(name, raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	return parseOptionalURL(name, raw)
}

func parseOptionalURL(name, raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || value.Scheme == "" || value.Host == "" {
		return nil, fmt.Errorf("%s must be an absolute URL", name)
	}
	return value, nil
}

func sameOrigin(first, second *url.URL) bool {
	return strings.EqualFold(first.Scheme, second.Scheme) &&
		strings.EqualFold(first.Host, second.Host)
}
