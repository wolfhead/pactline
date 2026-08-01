package v1

import (
	"context"
	"fmt"

	baseapi "github.com/wolfhead/pactline/internal/api"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/application"
	"github.com/wolfhead/pactline/internal/domain"
)

func (h *Handler) ListTaskAttachments(
	ctx context.Context,
	params generated.ListTaskAttachmentsParams,
) (generated.ListTaskAttachmentsRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	task, err := h.Access.RequireTaskByNumber(ctx, params.Number, subject, application.ProjectPermissionRead)
	if err != nil {
		return nil, err
	}
	attachments, err := h.Attachments.Attachments.List(ctx, task.Task.ID)
	if err != nil {
		return nil, err
	}
	items := make([]generated.TaskAttachment, len(attachments))
	for index, attachment := range attachments {
		items[index] = taskAttachmentFromDomain(params.Number, attachment)
	}
	return &generated.TaskAttachmentListHeaders{
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   generated.TaskAttachmentList{Items: items},
	}, nil
}

func (h *Handler) CreateTaskAttachmentUpload(
	ctx context.Context,
	req *generated.TaskAttachmentUploadWrite,
	params generated.CreateTaskAttachmentUploadParams,
) (generated.CreateTaskAttachmentUploadRes, error) {
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	task, err := h.Access.RequireTaskByNumber(ctx, params.Number, subject, application.ProjectPermissionWrite)
	if err != nil {
		return nil, err
	}
	upload, err := h.Attachments.StartUpload(
		ctx, task.Task.ID, subject.UserID, req.Filename, req.MediaType, req.SizeBytes,
	)
	if err != nil {
		return nil, err
	}
	uploadURL := upload.Instruction.URL
	if !upload.Instruction.Direct {
		uploadURL = fmt.Sprintf(
			"/api/v1/tasks/%d/attachments/uploads/%s/content", params.Number, upload.Session.ID,
		)
	}
	headers := generated.TaskAttachmentUploadHeaders{}
	for name, values := range upload.Instruction.Headers {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}
	response := generated.TaskAttachmentUpload{
		ID:       upload.Session.ID,
		Provider: generated.TaskAttachmentUploadProvider(upload.Session.Provider),
		Filename: upload.Session.Filename, MediaType: upload.Session.MediaType,
		SizeBytes: upload.Session.ExpectedSize, Direct: upload.Instruction.Direct,
		Method: generated.TaskAttachmentUploadMethodPUT, UploadURL: uploadURL,
		Headers: headers, ExpiresAt: upload.Session.ExpiresAt,
	}
	return &generated.TaskAttachmentUploadCreatedHeaders{
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/tasks/%d/attachments/uploads/%s", params.Number, upload.Session.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) CompleteTaskAttachmentUpload(
	ctx context.Context,
	params generated.CompleteTaskAttachmentUploadParams,
) (generated.CompleteTaskAttachmentUploadRes, error) {
	expectedTaskVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	actor, _, err := operationContext(ctx)
	if err != nil {
		return nil, err
	}
	task, err := h.Access.RequireTaskByNumber(ctx, params.Number, subject, application.ProjectPermissionWrite)
	if err != nil {
		return nil, err
	}
	session, err := h.Attachments.Attachments.GetUploadSession(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	if session.TaskID != task.Task.ID {
		return nil, domain.ErrNotFound
	}
	completed, err := h.Attachments.CompleteUpload(
		ctx, params.ID, subject.UserID, expectedTaskVersion, actor,
	)
	if err != nil {
		return nil, err
	}
	response := taskAttachmentFromDomain(params.Number, completed.Attachment)
	return &generated.TaskAttachmentCreatedHeaders{
		Etag: generated.NewOptString(formatETag(response.Version)),
		Location: generated.NewOptString(fmt.Sprintf(
			"/api/v1/tasks/%d/attachments/%s", params.Number, response.ID,
		)),
		XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx)),
		Response:   response,
	}, nil
}

func (h *Handler) DeleteTaskAttachment(
	ctx context.Context,
	params generated.DeleteTaskAttachmentParams,
) (generated.DeleteTaskAttachmentRes, error) {
	expectedVersion, err := parseIfMatch(params.IfMatch)
	if err != nil {
		return nil, err
	}
	subject, err := accessSubject(ctx)
	if err != nil {
		return nil, err
	}
	task, err := h.Access.RequireTaskByNumber(ctx, params.Number, subject, application.ProjectPermissionWrite)
	if err != nil {
		return nil, err
	}
	if _, err := h.Attachments.Attachments.SoftDelete(ctx, task.Task.ID, params.ID, expectedVersion); err != nil {
		return nil, err
	}
	return &generated.NoContent{XRequestID: generated.NewOptString(baseapi.RequestIDFromContext(ctx))}, nil
}

func taskAttachmentFromDomain(taskNumber int64, attachment domain.TaskAttachment) generated.TaskAttachment {
	baseURL := fmt.Sprintf("/api/v1/tasks/%d/attachments/%s/content", taskNumber, attachment.ID)
	return generated.TaskAttachment{
		ID: attachment.ID, TaskID: attachment.TaskID, UploaderID: attachment.UploaderID,
		Filename: attachment.Filename, MediaType: attachment.MediaType,
		SizeBytes:   attachment.SizeBytes,
		PreviewKind: generated.TaskAttachmentPreviewKind(domain.AttachmentPreview(attachment.Filename, attachment.MediaType)),
		Version:     attachment.Version, ContentURL: baseURL + "?disposition=inline",
		DownloadURL: baseURL + "?disposition=attachment",
		CreatedAt:   attachment.CreatedAt, UpdatedAt: attachment.UpdatedAt,
	}
}
