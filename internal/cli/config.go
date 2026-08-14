package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Server     string `json:"server"`
	Token      string `json:"token"`
	ClientKind string `json:"client_kind,omitempty"`
}

func configPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PACTLINE_CONFIG")); override != "" {
		return override, nil
	}
	directory, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(directory, ".pactline", "config.json"), nil
}

func loadConfig() (Config, string, error) {
	path, err := configPath()
	if err != nil {
		return Config{}, "", err
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, path, nil
	}
	if err != nil {
		return Config{}, path, fmt.Errorf("inspect config: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return Config{}, path, fmt.Errorf("config %s must not be accessible by group or others", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return Config{}, path, fmt.Errorf("read config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		return Config{}, path, fmt.Errorf("decode config: %w", err)
	}
	return config, path, nil
}

func saveConfig(config Config) (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return path, fmt.Errorf("create config directory: %w", err)
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		return path, fmt.Errorf("inspect config directory: %w", err)
	}
	if directoryInfo.Mode().Perm()&0o077 != 0 {
		return path, fmt.Errorf("config directory %s must not be accessible by group or others", directory)
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		return path, fmt.Errorf("refusing to overwrite insecure config %s", path)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return path, fmt.Errorf("inspect config: %w", statErr)
	}
	body, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return path, fmt.Errorf("encode config: %w", err)
	}
	body = append(body, '\n')
	temporary, err := os.CreateTemp(directory, ".config-*")
	if err != nil {
		return path, fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close() //nolint:errcheck
		return path, fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := temporary.Write(body); err != nil {
		temporary.Close() //nolint:errcheck
		return path, fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close() //nolint:errcheck
		return path, fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return path, fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return path, fmt.Errorf("replace config: %w", err)
	}
	return path, nil
}

func readSecret(reader io.Reader) (string, error) {
	body, err := io.ReadAll(io.LimitReader(reader, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read Token from stdin: %w", err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", errors.New("Token from stdin is empty")
	}
	return value, nil
}
