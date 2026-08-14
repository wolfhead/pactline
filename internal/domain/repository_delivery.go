package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RepositoryProvider string

const (
	RepositoryProviderGitLab RepositoryProvider = "gitlab"
	RepositoryProviderGitHub RepositoryProvider = "github"
)

func (p RepositoryProvider) Valid() bool {
	return p == RepositoryProviderGitLab || p == RepositoryProviderGitHub
}

type RepositoryReference struct {
	Provider          RepositoryProvider
	Origin            string
	PathWithNamespace string
	PathLookupKey     string
	WebURL            string
}

func (r RepositoryReference) Validate() error {
	if !r.Provider.Valid() || strings.TrimSpace(r.Origin) == "" ||
		strings.TrimSpace(r.PathWithNamespace) == "" || strings.TrimSpace(r.PathLookupKey) == "" ||
		strings.TrimSpace(r.WebURL) == "" {
		return fmt.Errorf("%w: repository reference is incomplete", ErrInvalidInput)
	}
	return nil
}

type RepositoryIdentity struct {
	ProviderRepositoryID string
	PathWithNamespace    string
	WebURL               string
	DefaultBranch        string
}

func (i RepositoryIdentity) Validate() error {
	if strings.TrimSpace(i.ProviderRepositoryID) == "" ||
		strings.TrimSpace(i.PathWithNamespace) == "" || strings.TrimSpace(i.WebURL) == "" {
		return fmt.Errorf("%w: repository identity is incomplete", ErrInvalidInput)
	}
	return nil
}

type RepositoryConnectionStatus string

const (
	RepositoryConnectionStatusActive   RepositoryConnectionStatus = "active"
	RepositoryConnectionStatusDisabled RepositoryConnectionStatus = "disabled"
)

func (s RepositoryConnectionStatus) Valid() bool {
	return s == RepositoryConnectionStatusActive || s == RepositoryConnectionStatusDisabled
}

type RepositoryConnection struct {
	ID                   uuid.UUID
	Version              int64
	Label                string
	Provider             RepositoryProvider
	Origin               string
	ProviderRepositoryID string
	PathWithNamespace    string
	PathLookupKey        string
	CanonicalWebURL      string
	DefaultBranch        string
	CredentialCiphertext []byte
	EncryptionKeyID      string
	CredentialExpiresAt  *time.Time
	Status               RepositoryConnectionStatus
	LastValidatedAt      time.Time
	CreatedBy            uuid.UUID
	DisabledBy           *uuid.UUID
	DisabledAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (c RepositoryConnection) Validate() error {
	if c.ID == uuid.Nil || c.Version < 1 || strings.TrimSpace(c.Label) == "" ||
		strings.TrimSpace(c.ProviderRepositoryID) == "" || len(c.CredentialCiphertext) == 0 ||
		strings.TrimSpace(c.EncryptionKeyID) == "" || !c.Status.Valid() ||
		c.LastValidatedAt.IsZero() || c.CreatedBy == uuid.Nil || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Repository Connection is incomplete", ErrInvalidInput)
	}
	reference := RepositoryReference{
		Provider: c.Provider, Origin: c.Origin, PathWithNamespace: c.PathWithNamespace,
		PathLookupKey: c.PathLookupKey, WebURL: c.CanonicalWebURL,
	}
	if err := reference.Validate(); err != nil {
		return fmt.Errorf("%w: Repository Connection repository identity is inconsistent", ErrInvalidInput)
	}
	if (c.Status == RepositoryConnectionStatusActive && (c.DisabledBy != nil || c.DisabledAt != nil)) ||
		(c.Status == RepositoryConnectionStatusDisabled &&
			(c.DisabledBy == nil || *c.DisabledBy == uuid.Nil || c.DisabledAt == nil)) {
		return fmt.Errorf("%w: Repository Connection status metadata is inconsistent", ErrInvalidInput)
	}
	return nil
}

type ProjectRepository struct {
	ID           uuid.UUID
	ProjectID    uuid.UUID
	ConnectionID uuid.UUID
	BoundBy      uuid.UUID
	BoundAt      time.Time
	UnboundBy    *uuid.UUID
	UnboundAt    *time.Time
}

func (r ProjectRepository) Active() bool { return r.UnboundAt == nil }

func (r ProjectRepository) Validate() error {
	if r.ID == uuid.Nil || r.ProjectID == uuid.Nil || r.ConnectionID == uuid.Nil ||
		r.BoundBy == uuid.Nil || r.BoundAt.IsZero() {
		return fmt.Errorf("%w: Project repository binding is incomplete", ErrInvalidInput)
	}
	if (r.UnboundBy == nil) != (r.UnboundAt == nil) ||
		(r.UnboundBy != nil && (*r.UnboundBy == uuid.Nil || r.UnboundAt.Before(r.BoundAt))) {
		return fmt.Errorf("%w: Project repository unbind metadata is inconsistent", ErrInvalidInput)
	}
	return nil
}

type CodeChangeKind string

const (
	CodeChangeKindMergeRequest CodeChangeKind = "merge_request"
	CodeChangeKindPullRequest  CodeChangeKind = "pull_request"
)

func (k CodeChangeKind) Valid() bool {
	return k == CodeChangeKindMergeRequest || k == CodeChangeKindPullRequest
}

func (k CodeChangeKind) CompatibleWith(provider RepositoryProvider) bool {
	return provider == RepositoryProviderGitLab && k == CodeChangeKindMergeRequest ||
		provider == RepositoryProviderGitHub && k == CodeChangeKindPullRequest
}

type CodeChangeObservationStatus string

const (
	CodeChangeObservationConfirmed    CodeChangeObservationStatus = "confirmed"
	CodeChangeObservationMissing      CodeChangeObservationStatus = "missing"
	CodeChangeObservationUnauthorized CodeChangeObservationStatus = "unauthorized"
	CodeChangeObservationUnreachable  CodeChangeObservationStatus = "unreachable"
	CodeChangeObservationDisconnected CodeChangeObservationStatus = "disconnected"
)

func (s CodeChangeObservationStatus) Valid() bool {
	switch s {
	case CodeChangeObservationConfirmed, CodeChangeObservationMissing, CodeChangeObservationUnauthorized,
		CodeChangeObservationUnreachable, CodeChangeObservationDisconnected:
		return true
	default:
		return false
	}
}

type CodeChangeState string

const (
	CodeChangeStateOpened CodeChangeState = "opened"
	CodeChangeStateClosed CodeChangeState = "closed"
	CodeChangeStateMerged CodeChangeState = "merged"
	CodeChangeStateLocked CodeChangeState = "locked"
)

func (s CodeChangeState) Valid() bool {
	switch s {
	case CodeChangeStateOpened, CodeChangeStateClosed, CodeChangeStateMerged, CodeChangeStateLocked:
		return true
	default:
		return false
	}
}

type CodeChangeObservation struct {
	Status            CodeChangeObservationStatus
	ObservedAt        time.Time
	Title             string
	State             CodeChangeState
	Draft             bool
	SourceBranch      string
	TargetBranch      string
	HeadSHA           string
	MergeCommitSHA    *string
	MergedAt          *time.Time
	ProviderUpdatedAt time.Time
}

func (o CodeChangeObservation) Validate() error {
	if !o.Status.Valid() || o.ObservedAt.IsZero() {
		return fmt.Errorf("%w: code change observation is incomplete", ErrInvalidInput)
	}
	if o.Status != CodeChangeObservationConfirmed {
		return nil
	}
	if strings.TrimSpace(o.Title) == "" || !o.State.Valid() ||
		strings.TrimSpace(o.SourceBranch) == "" || strings.TrimSpace(o.TargetBranch) == "" ||
		strings.TrimSpace(o.HeadSHA) == "" || o.ProviderUpdatedAt.IsZero() {
		return fmt.Errorf("%w: confirmed code change metadata is incomplete", ErrInvalidInput)
	}
	return nil
}

type CodeChangeReference struct {
	Repository   RepositoryReference
	Kind         CodeChangeKind
	ChangeNumber int64
	WebURL       string
}

func (r CodeChangeReference) Validate() error {
	if err := r.Repository.Validate(); err != nil {
		return err
	}
	if !r.Kind.CompatibleWith(r.Repository.Provider) || r.ChangeNumber < 1 || strings.TrimSpace(r.WebURL) == "" {
		return fmt.Errorf("%w: code change reference is incomplete or incompatible", ErrInvalidInput)
	}
	return nil
}

type CodeChange struct {
	Provider         RepositoryProvider
	Kind             CodeChangeKind
	ProviderChangeID string
	ChangeNumber     int64
	WebURL           string
	Observation      CodeChangeObservation
}

func (c CodeChange) Validate() error {
	if !c.Kind.CompatibleWith(c.Provider) || strings.TrimSpace(c.ProviderChangeID) == "" ||
		c.ChangeNumber < 1 || strings.TrimSpace(c.WebURL) == "" {
		return fmt.Errorf("%w: code change identity is incomplete or incompatible", ErrInvalidInput)
	}
	return c.Observation.Validate()
}

type TaskCodeChange struct {
	ID                     uuid.UUID
	TaskID                 uuid.UUID
	ProjectID              uuid.UUID
	ProjectRepositoryID    uuid.UUID
	Provider               RepositoryProvider
	Kind                   CodeChangeKind
	ChangeNumber           int64
	ProviderChangeID       string
	WebURL                 string
	LinkedBy               Actor
	LinkedThroughClaimID   uuid.UUID
	LinkedAt               time.Time
	UnlinkedBy             *Actor
	UnlinkedThroughClaimID *uuid.UUID
	UnlinkedAt             *time.Time
	LatestObservation      CodeChangeObservation
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (c TaskCodeChange) Active() bool { return c.UnlinkedAt == nil }

func (c TaskCodeChange) Validate() error {
	if c.ID == uuid.Nil || c.TaskID == uuid.Nil || c.ProjectID == uuid.Nil ||
		c.ProjectRepositoryID == uuid.Nil || !c.Kind.CompatibleWith(c.Provider) ||
		c.ChangeNumber < 1 || strings.TrimSpace(c.ProviderChangeID) == "" || strings.TrimSpace(c.WebURL) == "" ||
		!validDeliveryActor(c.LinkedBy) || c.LinkedThroughClaimID == uuid.Nil ||
		c.LinkedAt.IsZero() || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Task code change link is incomplete", ErrInvalidInput)
	}
	if err := c.LatestObservation.Validate(); err != nil {
		return err
	}
	if (c.UnlinkedBy == nil) != (c.UnlinkedAt == nil) ||
		(c.UnlinkedThroughClaimID == nil) != (c.UnlinkedAt == nil) {
		return fmt.Errorf("%w: Task code change unlink metadata is inconsistent", ErrInvalidInput)
	}
	if c.UnlinkedBy != nil &&
		(!validDeliveryActor(*c.UnlinkedBy) || *c.UnlinkedThroughClaimID == uuid.Nil ||
			c.UnlinkedAt.Before(c.LinkedAt)) {
		return fmt.Errorf("%w: Task code change unlink metadata is invalid", ErrInvalidInput)
	}
	return nil
}

func validDeliveryActor(actor Actor) bool {
	return (actor.Type == ActorTypeUser && actor.UserID != nil && *actor.UserID != uuid.Nil && actor.Ref == "") ||
		(actor.Type == ActorTypeAgent && actor.UserID == nil && strings.TrimSpace(actor.Ref) != "")
}

type CodeChangeSnapshot struct {
	TaskCodeChangeID     uuid.UUID                   `json:"task_code_change_id"`
	ProjectRepositoryID  uuid.UUID                   `json:"project_repository_id"`
	ConnectionID         uuid.UUID                   `json:"connection_id"`
	Provider             RepositoryProvider          `json:"provider"`
	ProviderRepositoryID string                      `json:"provider_repository_id"`
	Kind                 CodeChangeKind              `json:"kind"`
	ChangeNumber         int64                       `json:"change_number"`
	ProviderChangeID     string                      `json:"provider_change_id"`
	WebURL               string                      `json:"web_url"`
	Title                string                      `json:"title"`
	State                CodeChangeState             `json:"state"`
	Draft                bool                        `json:"draft"`
	SourceBranch         string                      `json:"source_branch"`
	TargetBranch         string                      `json:"target_branch"`
	HeadSHA              string                      `json:"head_sha"`
	MergeCommitSHA       *string                     `json:"merge_commit_sha,omitempty"`
	MergedAt             *time.Time                  `json:"merged_at,omitempty"`
	ObservationStatus    CodeChangeObservationStatus `json:"observation_status"`
	ObservedAt           time.Time                   `json:"observed_at"`
}

func (s CodeChangeSnapshot) Validate() error {
	if s.TaskCodeChangeID == uuid.Nil || s.ProjectRepositoryID == uuid.Nil || s.ConnectionID == uuid.Nil ||
		!s.Kind.CompatibleWith(s.Provider) || strings.TrimSpace(s.ProviderRepositoryID) == "" ||
		s.ChangeNumber < 1 || strings.TrimSpace(s.ProviderChangeID) == "" || strings.TrimSpace(s.WebURL) == "" ||
		!s.ObservationStatus.Valid() || s.ObservedAt.IsZero() {
		return fmt.Errorf("%w: code change snapshot is incomplete", ErrInvalidInput)
	}
	if s.ObservationStatus == CodeChangeObservationConfirmed &&
		(strings.TrimSpace(s.Title) == "" || !s.State.Valid() || strings.TrimSpace(s.HeadSHA) == "") {
		return fmt.Errorf("%w: confirmed code change snapshot is incomplete", ErrInvalidInput)
	}
	return nil
}
