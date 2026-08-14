package v1

import (
	"context"
	"errors"
	"fmt"

	"github.com/wolfhead/pactline/internal/access"
	generated "github.com/wolfhead/pactline/internal/api/v1generated"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/identity"

	"github.com/ogen-go/ogen/ogenerrors"
)

var (
	ErrAuthenticationRequired = errors.New("authentication required")
	ErrInsufficientScope      = errors.New("insufficient scope")
	ErrInvalidRequest         = errors.New("invalid request")
	ErrPreconditionRequired   = errors.New("precondition required")
)

type Security struct{}

func (Security) HandleBearerAuth(
	ctx context.Context,
	operation generated.OperationName,
	_ generated.BearerAuth,
) (context.Context, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return ctx, ErrAuthenticationRequired
	}
	if current.AuthenticationMethod != access.AuthenticationMethodAPIToken &&
		current.AuthenticationMethod != access.AuthenticationMethodAgentDelegate {
		return ctx, ogenerrors.ErrSkipServerSecurity
	}
	principal := access.Principal{Scopes: current.Scopes}
	if !principal.HasScope(requiredScope(operation)) {
		return ctx, ErrInsufficientScope
	}
	return ctx, nil
}

func (Security) HandleSessionCookie(
	ctx context.Context,
	_ generated.OperationName,
	_ generated.SessionCookie,
) (context.Context, error) {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return ctx, ErrAuthenticationRequired
	}
	if current.AuthenticationMethod != access.AuthenticationMethodSession {
		return ctx, ogenerrors.ErrSkipServerSecurity
	}
	return ctx, nil
}

func requiredScope(operation generated.OperationName) access.Scope {
	switch operation {
	case generated.AcceptTaskOperation,
		generated.CancelTaskOperation,
		generated.CreateTaskStageClaimOperation,
		generated.CreateTaskThreadMessageOperation,
		generated.DeleteTaskThreadMessageOperation,
		generated.MarkTaskReadyOperation,
		generated.LinkTaskCodeChangeOperation,
		generated.RecordTaskClaimProgressOperation,
		generated.RecordTaskStageAcceptanceCheckOperation,
		generated.RecordTaskWorkSubmissionOperation,
		generated.ReleaseTaskStageClaimOperation,
		generated.RequestTaskChangesOperation,
		generated.RequestTaskResolutionOperation,
		generated.ResolveTaskIssueOperation,
		generated.CompleteTaskExecutionOperation,
		generated.UpdateTaskThreadMessageOperation,
		generated.UnlinkTaskCodeChangeOperation,
		generated.WithdrawTaskReadinessOperation:
		return access.ScopeWorkExecute
	case generated.GetCurrentPrincipalOperation,
		generated.GetClaimWorkPacketOperation,
		generated.GetTaskStageClaimOperation,
		generated.GetAgentConversationOperation,
		generated.GetCurrentAgentConversationConfigurationOperation,
		generated.GetProjectOperation,
		generated.GetTaskDeliveryOperation,
		generated.GetTaskOperation,
		generated.GetTaskWorkPacketOperation,
		generated.ListLabelsOperation,
		generated.ListAgentConversationsOperation,
		generated.ListMilestoneCriteriaOperation,
		generated.ListProjectMembersOperation,
		generated.ListProjectRepositoriesOperation,
		generated.ListProjectsOperation,
		generated.ListTaskActivityOperation,
		generated.ListTaskAttachmentsOperation,
		generated.GetTaskAttachmentContentOperation,
		generated.ListTaskCriteriaOperation,
		generated.ListTaskStageClaimsOperation,
		generated.ListOwnedTaskStageClaimsOperation,
		generated.ListTaskThreadItemsOperation,
		generated.ListTaskThreadsOperation,
		generated.ListTasksOperation,
		generated.ListUsersOperation:
		return access.ScopeWorkRead
	default:
		return access.ScopeWorkWrite
	}
}

func requireAdministrator(ctx context.Context) error {
	current, ok := identity.FromContext(ctx)
	if !ok {
		return ErrAuthenticationRequired
	}
	if !current.Subject.Active || current.Subject.PlatformRole != domain.PlatformRoleAdmin {
		return fmt.Errorf("%w: Administrator access is required", domain.ErrForbidden)
	}
	return nil
}
