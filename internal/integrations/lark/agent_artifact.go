package lark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	reference := registered.Reference
	extension := filepath.Ext(reference.Name)
	temporary, err := os.CreateTemp("", "pactline-artifact-*"+extension)
	if err != nil {
		return artifact.LocalFile{}, fmt.Errorf("create Lark artifact temporary file: %w", err)
	}
	pathName := temporary.Name()
	cleanup := func() error { return os.Remove(pathName) }
	result, downloadErr := c.transport.Download(ctx, DownloadCall{
		Descriptor: descriptorFor("download_agent_artifact", http.MethodGet),
		Path:       path, Token: token, Target: temporary, MaxBytes: artifact.MaxFileSize,
	})
	closeErr := temporary.Close()
	if downloadErr != nil || closeErr != nil {
		_ = cleanup()
		switch {
		case errors.Is(downloadErr, errResponseTooLarge):
			return artifact.LocalFile{}, artifact.ErrTooLarge
		case downloadErr != nil:
			return artifact.LocalFile{}, fmt.Errorf("download Lark artifact: %w", downloadErr)
		case closeErr != nil:
			return artifact.LocalFile{}, fmt.Errorf("close Lark artifact temporary file: %w", closeErr)
		}
	}
	mediaType, _, _ := mime.ParseMediaType(result.ContentType)
	if mediaType != "" && mediaType != "application/octet-stream" {
		reference.MediaType = mediaType
	}
	slog.Debug("Lark Agent artifact downloaded",
		"artifact_id", artifactID,
		"size_bytes", result.ResponseBytes,
		"media_type", reference.MediaType,
		"request_id", result.ProviderRequestID)
	return artifact.LocalFile{Reference: reference, Path: pathName, Cleanup: cleanup}, nil
}

var _ artifact.Resolver = (*Client)(nil)
