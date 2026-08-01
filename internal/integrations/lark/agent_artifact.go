package lark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/channel"

	"github.com/google/uuid"
)

type rawMessageArtifact struct {
	ProviderKey  string
	ResourceType string
	Kind         artifact.Kind
	Name         string
	MediaType    string
}

type registeredArtifact struct {
	Reference      artifact.Reference
	TenantID       string
	ConversationID string
	MessageID      string
	ProviderKey    string
	ResourceType   string
	CreatedAt      time.Time
}

func artifactKindForName(name string) artifact.Kind {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".md", ".markdown":
		return artifact.KindMarkdown
	case ".csv":
		return artifact.KindCSV
	case ".xlsx":
		return artifact.KindSpreadsheet
	case ".txt":
		return artifact.KindText
	default:
		return artifact.KindFile
	}
}

func (c *Client) registerArtifacts(
	tenantID string,
	conversationID string,
	messageID string,
	createdAt time.Time,
	raw []rawMessageArtifact,
) []artifact.Reference {
	if len(raw) == 0 {
		return nil
	}
	references := make([]artifact.Reference, 0, len(raw))
	c.artifactMu.Lock()
	defer c.artifactMu.Unlock()
	if len(c.artifacts) > 1000 {
		cutoff := c.now().UTC().Add(-channel.MaxContextAge)
		for id, registered := range c.artifacts {
			if registered.CreatedAt.Before(cutoff) {
				delete(c.artifacts, id)
			}
		}
	}
	for index, item := range raw {
		if strings.TrimSpace(item.ProviderKey) == "" ||
			(item.ResourceType != "image" && item.ResourceType != "file") {
			continue
		}
		hash := sha256.Sum256([]byte(strings.Join([]string{
			tenantID, conversationID, messageID, item.ResourceType,
			item.ProviderKey, fmt.Sprintf("%d", index),
		}, "\x00")))
		id := "lark-artifact-" + hex.EncodeToString(hash[:12])
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.ResourceType + "-" + fmt.Sprintf("%d", index+1)
		}
		reference := artifact.Reference{
			ID: id, Kind: item.Kind, Name: name,
			MediaType:    strings.TrimSpace(item.MediaType),
			Availability: artifact.AvailabilityAvailable,
		}
		c.artifacts[id] = registeredArtifact{
			Reference: reference, TenantID: tenantID,
			ConversationID: conversationID, MessageID: messageID,
			ProviderKey: item.ProviderKey, ResourceType: item.ResourceType,
			CreatedAt: createdAt.UTC(),
		}
		references = append(references, reference)
	}
	return references
}

func (c *Client) Resolve(
	ctx context.Context,
	scope artifact.Scope,
	artifactID string,
) (artifact.LocalFile, error) {
	if c == nil || scope.RunID == uuid.Nil ||
		strings.TrimSpace(artifactID) == "" {
		return artifact.LocalFile{}, artifact.ErrInvalid
	}
	c.artifactMu.RLock()
	registered, ok := c.artifacts[artifactID]
	c.artifactMu.RUnlock()
	if !ok {
		return artifact.LocalFile{}, artifact.ErrNotFound
	}
	if registered.TenantID != scope.TenantID ||
		registered.ConversationID != scope.ConversationID ||
		registered.CreatedAt.Before(scope.NotBefore) ||
		registered.CreatedAt.After(scope.NotAfter) {
		return artifact.LocalFile{}, artifact.ErrScope
	}
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return artifact.LocalFile{}, fmt.Errorf("resolve Lark artifact token: %w", err)
	}
	path := "/open-apis/im/v1/messages/" + url.PathEscape(registered.MessageID) +
		"/resources/" + url.PathEscape(registered.ProviderKey) +
		"?type=" + url.QueryEscape(registered.ResourceType)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return artifact.LocalFile{}, fmt.Errorf("construct Lark artifact request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return artifact.LocalFile{}, fmt.Errorf("download Lark artifact: %w", err)
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Tt-Logid")
	if response.StatusCode != http.StatusOK {
		slog.Warn("Lark Agent artifact download rejected",
			"artifact_id", artifactID,
			"status", response.StatusCode,
			"request_id", requestID)
		return artifact.LocalFile{}, fmt.Errorf("download Lark artifact: provider HTTP %d", response.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	reference := registered.Reference
	if mediaType != "" && mediaType != "application/octet-stream" {
		reference.MediaType = mediaType
	}
	extension := filepath.Ext(reference.Name)
	temporary, err := os.CreateTemp("", "pactline-artifact-*"+extension)
	if err != nil {
		return artifact.LocalFile{}, fmt.Errorf("create Lark artifact temporary file: %w", err)
	}
	pathName := temporary.Name()
	cleanup := func() error { return os.Remove(pathName) }
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, artifact.MaxFileSize+1))
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil || written > artifact.MaxFileSize {
		_ = cleanup()
		switch {
		case copyErr != nil:
			return artifact.LocalFile{}, fmt.Errorf("write Lark artifact temporary file: %w", copyErr)
		case closeErr != nil:
			return artifact.LocalFile{}, fmt.Errorf("close Lark artifact temporary file: %w", closeErr)
		default:
			return artifact.LocalFile{}, artifact.ErrTooLarge
		}
	}
	slog.Debug("Lark Agent artifact downloaded",
		"artifact_id", artifactID,
		"size_bytes", written,
		"media_type", reference.MediaType,
		"request_id", requestID)
	return artifact.LocalFile{Reference: reference, Path: pathName, Cleanup: cleanup}, nil
}

var _ artifact.Resolver = (*Client)(nil)
