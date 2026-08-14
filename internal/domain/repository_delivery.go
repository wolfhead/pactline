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
	ID                uuid.UUID
	ProjectID         uuid.UUID
	Provider          RepositoryProvider
	Origin            string
	PathWithNamespace string
	PathLookupKey     string
	CanonicalWebURL   string
	BoundBy           uuid.UUID
	BoundAt           time.Time
	UnboundBy         *uuid.UUID
	UnboundAt         *time.Time
}

func (r ProjectRepository) Active() bool { return r.UnboundAt == nil }

func (r ProjectRepository) Validate() error {
	if r.ID == uuid.Nil || r.ProjectID == uuid.Nil || r.BoundBy == uuid.Nil || r.BoundAt.IsZero() {
		return fmt.Errorf("%w: Project repository binding is incomplete", ErrInvalidInput)
	}
	if err := r.Reference().Validate(); err != nil {
		return fmt.Errorf("%w: Project repository identity is incomplete", ErrInvalidInput)
	}
	if (r.UnboundBy == nil) != (r.UnboundAt == nil) ||
		(r.UnboundBy != nil && (*r.UnboundBy == uuid.Nil || r.UnboundAt.Before(r.BoundAt))) {
		return fmt.Errorf("%w: Project repository unbind metadata is inconsistent", ErrInvalidInput)
	}
	return nil
}

func (r ProjectRepository) Reference() RepositoryReference {
	return RepositoryReference{
		Provider: r.Provider, Origin: r.Origin, PathWithNamespace: r.PathWithNamespace,
		PathLookupKey: r.PathLookupKey, WebURL: r.CanonicalWebURL,
	}
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

type CodeChangeVerificationStatus string

const (
	CodeChangeVerificationVerified     CodeChangeVerificationStatus = "verified"
	CodeChangeVerificationMissing      CodeChangeVerificationStatus = "missing"
	CodeChangeVerificationUnauthorized CodeChangeVerificationStatus = "unauthorized"
	CodeChangeVerificationUnreachable  CodeChangeVerificationStatus = "unreachable"
	CodeChangeVerificationDisconnected CodeChangeVerificationStatus = "disconnected"
)

func (s CodeChangeVerificationStatus) Valid() bool {
	switch s {
	case CodeChangeVerificationVerified, CodeChangeVerificationMissing,
		CodeChangeVerificationUnauthorized, CodeChangeVerificationUnreachable,
		CodeChangeVerificationDisconnected:
		return true
	default:
		return false
	}
}

type CodeChangeVerification struct {
	Status      CodeChangeVerificationStatus
	AttemptedAt time.Time
}

func (v CodeChangeVerification) Validate() error {
	if !v.Status.Valid() || v.AttemptedAt.IsZero() {
		return fmt.Errorf("%w: code change verification is incomplete", ErrInvalidInput)
	}
	return nil
}

type CodeChangeProviderEvidence struct {
	ConnectionID         uuid.UUID       `json:"connection_id"`
	ProviderRepositoryID string          `json:"provider_repository_id"`
	ProviderChangeID     string          `json:"provider_change_id"`
	Title                string          `json:"title"`
	State                CodeChangeState `json:"state"`
	Draft                bool            `json:"draft"`
	SourceBranch         string          `json:"source_branch"`
	TargetBranch         string          `json:"target_branch"`
	HeadSHA              string          `json:"head_sha"`
	MergeCommitSHA       *string         `json:"merge_commit_sha,omitempty"`
	MergedAt             *time.Time      `json:"merged_at,omitempty"`
	ProviderUpdatedAt    time.Time       `json:"provider_updated_at"`
	ObservedAt           time.Time       `json:"observed_at"`
}

func (e CodeChangeProviderEvidence) Validate() error {
	if e.ConnectionID == uuid.Nil || strings.TrimSpace(e.ProviderRepositoryID) == "" ||
		strings.TrimSpace(e.ProviderChangeID) == "" || strings.TrimSpace(e.Title) == "" ||
		!e.State.Valid() || strings.TrimSpace(e.SourceBranch) == "" ||
		strings.TrimSpace(e.TargetBranch) == "" || strings.TrimSpace(e.HeadSHA) == "" ||
		e.ProviderUpdatedAt.IsZero() || e.ObservedAt.IsZero() {
		return fmt.Errorf("%w: code change provider evidence is incomplete", ErrInvalidInput)
	}
	return nil
}

type TaskCodeChange struct {
	ID                     uuid.UUID
	TaskID                 uuid.UUID
	ProjectID              uuid.UUID
	ProjectRepositoryID    uuid.UUID
	Provider               RepositoryProvider
	Kind                   CodeChangeKind
	ChangeNumber           int64
	WebURL                 string
	LinkedBy               Actor
	LinkedThroughClaimID   uuid.UUID
	LinkedAt               time.Time
	UnlinkedBy             *Actor
	UnlinkedThroughClaimID *uuid.UUID
	UnlinkedAt             *time.Time
	ProviderEvidence       *CodeChangeProviderEvidence
	ProviderVerification   *CodeChangeVerification
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (c TaskCodeChange) Active() bool { return c.UnlinkedAt == nil }

func (c TaskCodeChange) Validate() error {
	if c.ID == uuid.Nil || c.TaskID == uuid.Nil || c.ProjectID == uuid.Nil ||
		c.ProjectRepositoryID == uuid.Nil || !c.Kind.CompatibleWith(c.Provider) ||
		c.ChangeNumber < 1 || strings.TrimSpace(c.WebURL) == "" ||
		!validDeliveryActor(c.LinkedBy) || c.LinkedThroughClaimID == uuid.Nil ||
		c.LinkedAt.IsZero() || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Task code change link is incomplete", ErrInvalidInput)
	}
	if c.ProviderEvidence != nil {
		if err := c.ProviderEvidence.Validate(); err != nil {
			return err
		}
	}
	if c.ProviderVerification != nil {
		if err := c.ProviderVerification.Validate(); err != nil {
			return err
		}
		if c.ProviderVerification.Status == CodeChangeVerificationVerified && c.ProviderEvidence == nil {
			return fmt.Errorf("%w: verified code change requires provider evidence", ErrInvalidInput)
		}
	}
	if c.ProviderEvidence != nil && c.ProviderVerification == nil {
		return fmt.Errorf("%w: code change provider evidence requires verification", ErrInvalidInput)
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
	TaskCodeChangeID    uuid.UUID                   `json:"task_code_change_id"`
	ProjectRepositoryID uuid.UUID                   `json:"project_repository_id"`
	Provider            RepositoryProvider          `json:"provider"`
	Kind                CodeChangeKind              `json:"kind"`
	ChangeNumber        int64                       `json:"change_number"`
	WebURL              string                      `json:"web_url"`
	ProviderEvidence    *CodeChangeProviderEvidence `json:"provider_evidence,omitempty"`
}

func (s CodeChangeSnapshot) Validate() error {
	if s.TaskCodeChangeID == uuid.Nil || s.ProjectRepositoryID == uuid.Nil ||
		!s.Kind.CompatibleWith(s.Provider) || s.ChangeNumber < 1 || strings.TrimSpace(s.WebURL) == "" {
		return fmt.Errorf("%w: code change snapshot is incomplete", ErrInvalidInput)
	}
	if s.ProviderEvidence != nil {
		if err := s.ProviderEvidence.Validate(); err != nil {
			return err
		}
	}
	return nil
}
