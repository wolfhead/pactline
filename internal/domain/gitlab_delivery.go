package domain

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type GitLabRepositoryReference struct {
	Origin            string
	PathWithNamespace string
	PathLookupKey     string
	WebURL            string
}

func ParseGitLabRepositoryURL(raw string) (GitLabRepositoryReference, error) {
	parsed, err := parseGitLabHTTPSURL(raw)
	if err != nil {
		return GitLabRepositoryReference{}, err
	}
	if strings.Contains(parsed.Path, "/-/") {
		return GitLabRepositoryReference{}, fmt.Errorf(
			"%w: GitLab repository URL must not contain a subpage", ErrInvalidInput,
		)
	}
	repositoryPath, err := normalizeGitLabRepositoryPath(parsed.EscapedPath())
	if err != nil {
		return GitLabRepositoryReference{}, err
	}
	origin := gitLabOrigin(parsed)
	canonical := &url.URL{Scheme: "https", Host: parsed.Host, Path: "/" + repositoryPath}
	return GitLabRepositoryReference{
		Origin:            origin,
		PathWithNamespace: repositoryPath,
		PathLookupKey:     strings.ToLower(repositoryPath),
		WebURL:            canonical.String(),
	}, nil
}

type GitLabMergeRequestReference struct {
	Repository GitLabRepositoryReference
	IID        int64
	WebURL     string
}

func ParseGitLabMergeRequestURL(raw string) (GitLabMergeRequestReference, error) {
	parsed, err := parseGitLabHTTPSURL(raw)
	if err != nil {
		return GitLabMergeRequestReference{}, err
	}
	const marker = "/-/merge_requests/"
	markerIndex := strings.LastIndex(parsed.Path, marker)
	if markerIndex <= 0 {
		return GitLabMergeRequestReference{}, fmt.Errorf(
			"%w: GitLab merge request URL is invalid", ErrInvalidInput,
		)
	}
	iidText := parsed.Path[markerIndex+len(marker):]
	if iidText == "" || strings.Contains(iidText, "/") {
		return GitLabMergeRequestReference{}, fmt.Errorf(
			"%w: GitLab merge request URL is invalid", ErrInvalidInput,
		)
	}
	iid, err := strconv.ParseInt(iidText, 10, 64)
	if err != nil || iid < 1 {
		return GitLabMergeRequestReference{}, fmt.Errorf(
			"%w: GitLab merge request IID is invalid", ErrInvalidInput,
		)
	}
	repositoryURL := &url.URL{
		Scheme: "https",
		Host:   parsed.Host,
		Path:   parsed.Path[:markerIndex],
	}
	repository, err := ParseGitLabRepositoryURL(repositoryURL.String())
	if err != nil {
		return GitLabMergeRequestReference{}, err
	}
	canonical := &url.URL{
		Scheme: "https",
		Host:   parsed.Host,
		Path:   "/" + repository.PathWithNamespace + marker + strconv.FormatInt(iid, 10),
	}
	return GitLabMergeRequestReference{Repository: repository, IID: iid, WebURL: canonical.String()}, nil
}

func parseGitLabHTTPSURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, fmt.Errorf("%w: an HTTPS GitLab URL without credentials, query, or fragment is required", ErrInvalidInput)
	}
	if parsed.Hostname() == "" || net.ParseIP(parsed.Hostname()) == nil && strings.Contains(parsed.Hostname(), " ") {
		return nil, fmt.Errorf("%w: GitLab URL host is invalid", ErrInvalidInput)
	}
	parsed.Scheme = "https"
	parsed.Host = normalizedURLHost(parsed)
	return parsed, nil
}

func normalizedURLHost(parsed *url.URL) string {
	hostname := strings.ToLower(parsed.Hostname())
	if parsed.Port() == "" {
		return hostname
	}
	return net.JoinHostPort(hostname, parsed.Port())
}

func gitLabOrigin(parsed *url.URL) string {
	return (&url.URL{Scheme: "https", Host: parsed.Host}).String()
}

func normalizeGitLabRepositoryPath(escapedPath string) (string, error) {
	unescaped, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", fmt.Errorf("%w: GitLab repository path escaping is invalid", ErrInvalidInput)
	}
	unescaped = strings.TrimSuffix(unescaped, "/")
	unescaped = strings.TrimSuffix(unescaped, ".git")
	unescaped = strings.TrimPrefix(unescaped, "/")
	if unescaped == "" || path.Clean(unescaped) != unescaped || strings.Contains(unescaped, "//") {
		return "", fmt.Errorf("%w: GitLab repository path is invalid", ErrInvalidInput)
	}
	segments := strings.Split(unescaped, "/")
	if len(segments) < 2 {
		return "", fmt.Errorf("%w: GitLab repository path must include a namespace", ErrInvalidInput)
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("%w: GitLab repository path is invalid", ErrInvalidInput)
		}
	}
	return unescaped, nil
}

type GitLabConnectionStatus string

const (
	GitLabConnectionStatusActive   GitLabConnectionStatus = "active"
	GitLabConnectionStatusDisabled GitLabConnectionStatus = "disabled"
)

func (s GitLabConnectionStatus) Valid() bool {
	return s == GitLabConnectionStatusActive || s == GitLabConnectionStatusDisabled
}

type GitLabConnection struct {
	ID                   uuid.UUID
	Version              int64
	Label                string
	Origin               string
	GitLabProjectID      int64
	PathWithNamespace    string
	PathLookupKey        string
	CanonicalWebURL      string
	DefaultBranch        string
	CredentialCiphertext []byte
	EncryptionKeyID      string
	CredentialExpiresAt  *time.Time
	Status               GitLabConnectionStatus
	LastValidatedAt      time.Time
	CreatedBy            uuid.UUID
	DisabledBy           *uuid.UUID
	DisabledAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (c GitLabConnection) Validate() error {
	if c.ID == uuid.Nil || c.Version < 1 || strings.TrimSpace(c.Label) == "" ||
		c.GitLabProjectID < 1 || strings.TrimSpace(c.PathWithNamespace) == "" ||
		strings.TrimSpace(c.PathLookupKey) == "" || strings.TrimSpace(c.CanonicalWebURL) == "" ||
		len(c.CredentialCiphertext) == 0 || strings.TrimSpace(c.EncryptionKeyID) == "" ||
		!c.Status.Valid() || c.LastValidatedAt.IsZero() || c.CreatedBy == uuid.Nil ||
		c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: GitLab Connection is incomplete", ErrInvalidInput)
	}
	reference, err := ParseGitLabRepositoryURL(c.CanonicalWebURL)
	if err != nil || reference.Origin != c.Origin || reference.PathWithNamespace != c.PathWithNamespace ||
		reference.PathLookupKey != c.PathLookupKey {
		return fmt.Errorf("%w: GitLab Connection repository identity is inconsistent", ErrInvalidInput)
	}
	if (c.Status == GitLabConnectionStatusActive && (c.DisabledBy != nil || c.DisabledAt != nil)) ||
		(c.Status == GitLabConnectionStatusDisabled &&
			(c.DisabledBy == nil || *c.DisabledBy == uuid.Nil || c.DisabledAt == nil)) {
		return fmt.Errorf("%w: GitLab Connection status metadata is inconsistent", ErrInvalidInput)
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

type GitLabObservationStatus string

const (
	GitLabObservationConfirmed    GitLabObservationStatus = "confirmed"
	GitLabObservationMissing      GitLabObservationStatus = "missing"
	GitLabObservationUnauthorized GitLabObservationStatus = "unauthorized"
	GitLabObservationUnreachable  GitLabObservationStatus = "unreachable"
	GitLabObservationDisconnected GitLabObservationStatus = "disconnected"
)

func (s GitLabObservationStatus) Valid() bool {
	switch s {
	case GitLabObservationConfirmed, GitLabObservationMissing, GitLabObservationUnauthorized,
		GitLabObservationUnreachable, GitLabObservationDisconnected:
		return true
	default:
		return false
	}
}

type GitLabMergeRequestState string

const (
	GitLabMergeRequestOpened GitLabMergeRequestState = "opened"
	GitLabMergeRequestClosed GitLabMergeRequestState = "closed"
	GitLabMergeRequestMerged GitLabMergeRequestState = "merged"
	GitLabMergeRequestLocked GitLabMergeRequestState = "locked"
)

func (s GitLabMergeRequestState) Valid() bool {
	switch s {
	case GitLabMergeRequestOpened, GitLabMergeRequestClosed, GitLabMergeRequestMerged,
		GitLabMergeRequestLocked:
		return true
	default:
		return false
	}
}

type GitLabMergeRequestObservation struct {
	Status            GitLabObservationStatus
	ObservedAt        time.Time
	Title             string
	State             GitLabMergeRequestState
	Draft             bool
	SourceBranch      string
	TargetBranch      string
	HeadSHA           string
	MergeCommitSHA    *string
	MergedAt          *time.Time
	ProviderUpdatedAt time.Time
}

type GitLabProjectIdentity struct {
	ID                int64
	PathWithNamespace string
	WebURL            string
	DefaultBranch     string
}

func (p GitLabProjectIdentity) Validate() error {
	if p.ID < 1 || strings.TrimSpace(p.PathWithNamespace) == "" || strings.TrimSpace(p.WebURL) == "" {
		return fmt.Errorf("%w: GitLab project identity is incomplete", ErrInvalidInput)
	}
	return nil
}

type GitLabMergeRequest struct {
	ID          int64
	IID         int64
	WebURL      string
	Observation GitLabMergeRequestObservation
}

type TaskMergeRequest struct {
	ID                     uuid.UUID
	TaskID                 uuid.UUID
	ProjectID              uuid.UUID
	ProjectRepositoryID    uuid.UUID
	MergeRequestIID        int64
	GitLabMergeRequestID   int64
	WebURL                 string
	LinkedBy               Actor
	LinkedThroughClaimID   uuid.UUID
	LinkedAt               time.Time
	UnlinkedBy             *Actor
	UnlinkedThroughClaimID *uuid.UUID
	UnlinkedAt             *time.Time
	LatestObservation      GitLabMergeRequestObservation
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (m TaskMergeRequest) Active() bool { return m.UnlinkedAt == nil }

func (m TaskMergeRequest) Validate() error {
	if m.ID == uuid.Nil || m.TaskID == uuid.Nil || m.ProjectID == uuid.Nil ||
		m.ProjectRepositoryID == uuid.Nil || m.MergeRequestIID < 1 ||
		m.GitLabMergeRequestID < 1 || strings.TrimSpace(m.WebURL) == "" ||
		!validDeliveryActor(m.LinkedBy) || m.LinkedThroughClaimID == uuid.Nil ||
		m.LinkedAt.IsZero() || m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: Task merge request link is incomplete", ErrInvalidInput)
	}
	if err := m.LatestObservation.Validate(); err != nil {
		return err
	}
	reference, err := ParseGitLabMergeRequestURL(m.WebURL)
	if err != nil || reference.IID != m.MergeRequestIID {
		return fmt.Errorf("%w: Task merge request URL identity is inconsistent", ErrInvalidInput)
	}
	if (m.UnlinkedBy == nil) != (m.UnlinkedAt == nil) ||
		(m.UnlinkedThroughClaimID == nil) != (m.UnlinkedAt == nil) {
		return fmt.Errorf("%w: Task merge request unlink metadata is inconsistent", ErrInvalidInput)
	}
	if m.UnlinkedBy != nil &&
		(!validDeliveryActor(*m.UnlinkedBy) || *m.UnlinkedThroughClaimID == uuid.Nil ||
			m.UnlinkedAt.Before(m.LinkedAt)) {
		return fmt.Errorf("%w: Task merge request unlink metadata is invalid", ErrInvalidInput)
	}
	return nil
}

func validDeliveryActor(actor Actor) bool {
	return (actor.Type == ActorTypeUser && actor.UserID != nil && *actor.UserID != uuid.Nil && actor.Ref == "") ||
		(actor.Type == ActorTypeAgent && actor.UserID == nil && strings.TrimSpace(actor.Ref) != "")
}

func (m GitLabMergeRequest) Validate() error {
	if m.ID < 1 || m.IID < 1 || strings.TrimSpace(m.WebURL) == "" {
		return fmt.Errorf("%w: GitLab merge request identity is incomplete", ErrInvalidInput)
	}
	return m.Observation.Validate()
}

func (o GitLabMergeRequestObservation) Validate() error {
	if !o.Status.Valid() || o.ObservedAt.IsZero() {
		return fmt.Errorf("%w: GitLab merge request observation is incomplete", ErrInvalidInput)
	}
	if o.Status != GitLabObservationConfirmed {
		return nil
	}
	if strings.TrimSpace(o.Title) == "" || !o.State.Valid() ||
		strings.TrimSpace(o.SourceBranch) == "" || strings.TrimSpace(o.TargetBranch) == "" ||
		strings.TrimSpace(o.HeadSHA) == "" || o.ProviderUpdatedAt.IsZero() {
		return fmt.Errorf("%w: confirmed GitLab merge request metadata is incomplete", ErrInvalidInput)
	}
	return nil
}

type MergeRequestSnapshot struct {
	TaskMergeRequestID  uuid.UUID               `json:"task_merge_request_id"`
	ProjectRepositoryID uuid.UUID               `json:"project_repository_id"`
	ConnectionID        uuid.UUID               `json:"connection_id"`
	GitLabProjectID     int64                   `json:"gitlab_project_id"`
	MergeRequestIID     int64                   `json:"merge_request_iid"`
	WebURL              string                  `json:"web_url"`
	Title               string                  `json:"title"`
	State               GitLabMergeRequestState `json:"state"`
	Draft               bool                    `json:"draft"`
	SourceBranch        string                  `json:"source_branch"`
	TargetBranch        string                  `json:"target_branch"`
	HeadSHA             string                  `json:"head_sha"`
	MergeCommitSHA      *string                 `json:"merge_commit_sha,omitempty"`
	MergedAt            *time.Time              `json:"merged_at,omitempty"`
	ObservationStatus   GitLabObservationStatus `json:"observation_status"`
	ObservedAt          time.Time               `json:"observed_at"`
}

func (s MergeRequestSnapshot) Validate() error {
	if s.TaskMergeRequestID == uuid.Nil || s.ProjectRepositoryID == uuid.Nil ||
		s.ConnectionID == uuid.Nil || s.GitLabProjectID < 1 || s.MergeRequestIID < 1 ||
		strings.TrimSpace(s.WebURL) == "" || !s.ObservationStatus.Valid() || s.ObservedAt.IsZero() {
		return fmt.Errorf("%w: merge request snapshot is incomplete", ErrInvalidInput)
	}
	if s.ObservationStatus == GitLabObservationConfirmed &&
		(strings.TrimSpace(s.Title) == "" || !s.State.Valid() || strings.TrimSpace(s.HeadSHA) == "") {
		return fmt.Errorf("%w: confirmed merge request snapshot is incomplete", ErrInvalidInput)
	}
	reference, err := ParseGitLabMergeRequestURL(s.WebURL)
	if err != nil || reference.IID != s.MergeRequestIID {
		return fmt.Errorf("%w: merge request snapshot URL identity is inconsistent", ErrInvalidInput)
	}
	return nil
}
