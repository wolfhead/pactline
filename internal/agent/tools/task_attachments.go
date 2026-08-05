package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/wolfhead/pactline/internal/agent/artifact"
	"github.com/wolfhead/pactline/internal/agent/channel"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"

	"github.com/google/uuid"
)

const maxSourceArtifactsPerTask = 5

func normalizeSourceArtifactIDs(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w: source artifact ID is required", ErrToolInput)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) > maxSourceArtifactsPerTask {
			return nil, fmt.Errorf(
				"%w: source_artifact_ids accepts at most %d artifacts",
				ErrToolInput,
				maxSourceArtifactsPerTask,
			)
		}
	}
	return result, nil
}

func (t *CreateTaskTool) attachSourceArtifacts(
	ctx context.Context,
	task CreatedTask,
	taskVersion int64,
	artifactIDs []string,
) ([]AttachedArtifact, []AttachmentFailure) {
	client, clientReady := t.config.Client.(AttachmentOpenAPIClient)
	if t.config.Artifacts == nil || !clientReady {
		failures := make([]AttachmentFailure, len(artifactIDs))
		for index, artifactID := range artifactIDs {
			failures[index] = AttachmentFailure{
				ArtifactID: artifactID,
				Reason:     "attachment_import_unavailable",
			}
		}
		slog.Error("Agent artifact attachment import is unavailable",
			"run_id", t.config.Run.ID,
			"task_number", task.Number,
			"artifact_count", len(artifactIDs))
		return nil, failures
	}

	attached := make([]AttachedArtifact, 0, len(artifactIDs))
	failures := make([]AttachmentFailure, 0)
	currentVersion := taskVersion
	for _, artifactID := range artifactIDs {
		result, nextVersion, failure := t.attachSourceArtifact(
			ctx, client, task.Number, currentVersion, artifactID,
		)
		if failure != nil {
			failures = append(failures, *failure)
			continue
		}
		attached = append(attached, result)
		currentVersion = nextVersion
	}
	return attached, failures
}

func (t *CreateTaskTool) attachSourceArtifact(
	ctx context.Context,
	client AttachmentOpenAPIClient,
	taskNumber, taskVersion int64,
	artifactID string,
) (AttachedArtifact, int64, *AttachmentFailure) {
	scope := artifact.Scope{
		RunID:          t.config.Run.ID,
		TenantID:       t.config.Run.TenantID,
		ConversationID: t.config.Run.ConversationID,
		NotBefore:      t.config.Run.TriggerOccurredAt.Add(-channel.MaxContextAge),
		NotAfter:       t.config.Run.TriggerOccurredAt,
	}
	local, err := t.config.Artifacts.Resolve(ctx, scope, artifactID)
	if err != nil {
		t.logAttachmentFailure(taskNumber, artifactID, "resolve", err)
		return AttachedArtifact{}, taskVersion, &AttachmentFailure{
			ArtifactID: artifactID,
			Reason:     "artifact_unavailable",
		}
	}
	if local.Cleanup != nil {
		defer func() {
			if cleanupErr := local.Cleanup(); cleanupErr != nil {
				slog.Error("clean copied Agent artifact failed",
					"run_id", t.config.Run.ID,
					"task_number", taskNumber,
					"artifact_id", artifactID,
					"error", cleanupErr)
			}
		}()
	}

	source, err := artifact.PrepareAttachment(local)
	if err != nil {
		t.logAttachmentFailure(taskNumber, artifactID, "metadata", err)
		return AttachedArtifact{}, taskVersion, &AttachmentFailure{
			ArtifactID: artifactID,
			Filename:   strings.TrimSpace(local.Reference.Name),
			Reason:     "invalid_attachment",
		}
	}
	filename := source.Filename
	uploadResponse, err := client.CreateTaskAttachmentUpload(
		ctx,
		&generated.TaskAttachmentUploadWrite{
			Filename: filename, MediaType: source.MediaType, SizeBytes: source.SizeBytes,
		},
		generated.CreateTaskAttachmentUploadParams{
			Number: taskNumber,
			IdempotencyKey: generated.NewOptString(
				artifactImportIdempotencyKey(t.config.Run.ID, artifactID, "start"),
			),
		},
	)
	if err != nil {
		t.logAttachmentFailure(taskNumber, artifactID, "start_upload", err)
		return AttachedArtifact{}, taskVersion, attachmentFailure(artifactID, filename, "upload_session_failed")
	}
	uploadCreated, ok := uploadResponse.(*generated.TaskAttachmentUploadCreatedHeaders)
	if !ok {
		err = openAPIResponseError(uploadResponse)
		t.logAttachmentFailure(taskNumber, artifactID, "start_upload", err)
		return AttachedArtifact{}, taskVersion, attachmentFailure(artifactID, filename, "upload_session_failed")
	}
	upload := uploadCreated.Response
	if err := t.uploadArtifactContent(ctx, client, taskNumber, upload, source); err != nil {
		t.logAttachmentFailure(taskNumber, artifactID, "upload_content", err)
		return AttachedArtifact{}, taskVersion, attachmentFailure(artifactID, filename, "content_upload_failed")
	}
	completeResponse, err := client.CompleteTaskAttachmentUpload(
		ctx,
		generated.CompleteTaskAttachmentUploadParams{
			Number:  taskNumber,
			ID:      upload.ID,
			IfMatch: fmt.Sprintf("\"%d\"", taskVersion),
			IdempotencyKey: generated.NewOptString(
				artifactImportIdempotencyKey(t.config.Run.ID, artifactID, "complete"),
			),
		},
	)
	if err != nil {
		t.logAttachmentFailure(taskNumber, artifactID, "complete_upload", err)
		return AttachedArtifact{}, taskVersion, attachmentFailure(artifactID, filename, "completion_failed")
	}
	completed, ok := completeResponse.(*generated.TaskAttachmentCreatedHeaders)
	if !ok {
		err = openAPIResponseError(completeResponse)
		t.logAttachmentFailure(taskNumber, artifactID, "complete_upload", err)
		return AttachedArtifact{}, taskVersion, attachmentFailure(artifactID, filename, "completion_failed")
	}
	slog.Info("Agent conversation artifact attached to Task",
		"run_id", t.config.Run.ID,
		"task_number", taskNumber,
		"artifact_id", artifactID,
		"attachment_id", completed.Response.ID,
		"size_bytes", source.SizeBytes)
	return AttachedArtifact{
		ArtifactID:   artifactID,
		AttachmentID: completed.Response.ID,
		Filename:     completed.Response.Filename,
	}, taskVersion + 1, nil
}

func (t *CreateTaskTool) uploadArtifactContent(
	ctx context.Context,
	client AttachmentOpenAPIClient,
	taskNumber int64,
	upload generated.TaskAttachmentUpload,
	source artifact.AttachmentSource,
) error {
	file, err := source.Open()
	if err != nil {
		return fmt.Errorf("open resolved artifact: %w", err)
	}
	defer file.Close() //nolint:errcheck
	if !upload.Direct {
		response, uploadErr := client.UploadTaskAttachmentContent(
			ctx,
			generated.UploadTaskAttachmentContentReq{Data: file},
			generated.UploadTaskAttachmentContentParams{
				Number: taskNumber, ID: upload.ID, ContentLength: source.SizeBytes,
			},
		)
		if uploadErr != nil {
			return uploadErr
		}
		if _, ok := response.(*generated.NoContent); !ok {
			return openAPIResponseError(response)
		}
		return nil
	}

	uploadURL, err := url.Parse(upload.UploadURL)
	if err != nil || uploadURL.Scheme != "https" || uploadURL.Host == "" {
		return fmt.Errorf("direct upload URL is not an absolute HTTPS URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL.String(), file)
	if err != nil {
		return fmt.Errorf("construct direct attachment upload: %w", err)
	}
	request.ContentLength = source.SizeBytes
	for name, value := range upload.Headers {
		request.Header.Set(name, value)
	}
	doer := t.config.DirectUploadHTTP
	if doer == nil {
		doer = http.DefaultClient
	}
	response, err := doer.Do(request)
	if err != nil {
		return fmt.Errorf("direct attachment upload: %w", err)
	}
	defer response.Body.Close() //nolint:errcheck
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("direct attachment upload returned HTTP %d", response.StatusCode)
	}
	return nil
}

func artifactImportIdempotencyKey(runID uuid.UUID, artifactID, phase string) string {
	digest := sha256.Sum256([]byte(runID.String() + "\x00" + artifactID + "\x00" + phase))
	return "agent-artifact-" + phase + "-" + hex.EncodeToString(digest[:12])
}

func attachmentFailure(artifactID, filename, reason string) *AttachmentFailure {
	return &AttachmentFailure{ArtifactID: artifactID, Filename: filename, Reason: reason}
}

func (t *CreateTaskTool) logAttachmentFailure(
	taskNumber int64,
	artifactID, phase string,
	err error,
) {
	slog.Warn("Agent conversation artifact attachment failed",
		"run_id", t.config.Run.ID,
		"task_number", taskNumber,
		"artifact_id", artifactID,
		"phase", phase,
		"error", err)
}
