package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	EnvironmentDevelopment  = "development"
	EnvironmentTest         = "test"
	EnvironmentProduction   = "production"
	AuthProviderDevelopment = "development"
	AuthProviderLark        = "lark"
)

type Config struct {
	AppEnv               string
	AuthProvider         string
	AppBaseURL           *url.URL
	SessionSecret        []byte
	TokenEncryptionKey   []byte
	TokenEncryptionKeyID string
	LarkAppID            string
	LarkAppSecret        string
	LarkTenantKey        string
	LarkRedirectURI      *url.URL
	BootstrapAdminEmail  string
}

func LoadConfig() (Config, error) {
	cfg := Config{
		AppEnv:               strings.TrimSpace(os.Getenv("APP_ENV")),
		AuthProvider:         strings.TrimSpace(os.Getenv("AUTH_PROVIDER")),
		SessionSecret:        []byte(os.Getenv("SESSION_SECRET")),
		TokenEncryptionKeyID: strings.TrimSpace(os.Getenv("OAUTH_TOKEN_ENCRYPTION_KEY_ID")),
		LarkAppID:            strings.TrimSpace(os.Getenv("LARK_APP_ID")),
		LarkAppSecret:        os.Getenv("LARK_APP_SECRET"),
		LarkTenantKey:        strings.TrimSpace(os.Getenv("LARK_TENANT_KEY")),
		BootstrapAdminEmail:  strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")),
	}
	var err error
	cfg.AppBaseURL, err = parseConfiguredURL("APP_BASE_URL", os.Getenv("APP_BASE_URL"))
	if err != nil {
		return Config{}, err
	}
	cfg.LarkRedirectURI, err = parseOptionalURL("LARK_REDIRECT_URI", os.Getenv("LARK_REDIRECT_URI"))
	if err != nil {
		return Config{}, err
	}
	if encoded := os.Getenv("OAUTH_TOKEN_ENCRYPTION_KEY"); encoded != "" {
		cfg.TokenEncryptionKey, err = decodeEncryptionKey(encoded)
		if err != nil {
			return Config{}, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	if c.AppEnv == EnvironmentProduction {
		if c.AuthProvider == AuthProviderDevelopment {
			return errors.New("development authentication is not allowed in production")
		}
		if c.AppBaseURL.Scheme != "https" {
			return errors.New("APP_BASE_URL must use HTTPS in production")
		}
		if len(c.SessionSecret) < 32 {
			return errors.New("SESSION_SECRET must contain at least 32 bytes in production")
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
	return nil
}

func decodeEncryptionKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, errors.New("OAUTH_TOKEN_ENCRYPTION_KEY must be base64 encoded")
	}
	if len(key) != 32 {
		return nil, errors.New("OAUTH_TOKEN_ENCRYPTION_KEY must decode to exactly 32 bytes")
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
