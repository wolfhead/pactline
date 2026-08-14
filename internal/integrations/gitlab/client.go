package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/wolfhead/pactline/internal/domain"
)

const (
	defaultTimeout      = 10 * time.Second
	maxResponseBodySize = 1 << 20
)

type ErrorCategory string

const (
	ErrorInvalidReference ErrorCategory = "invalid_reference"
	ErrorNotFound         ErrorCategory = "not_found"
	ErrorUnauthorized     ErrorCategory = "unauthorized"
	ErrorUnreachable      ErrorCategory = "unreachable"
	ErrorProviderRejected ErrorCategory = "provider_rejected"
)

type ProviderError struct {
	Category          ErrorCategory
	StatusCode        int
	ProviderRequestID string
	Err               error
}

func (e *ProviderError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("GitLab %s (status %d)", e.Category, e.StatusCode)
	}
	return fmt.Sprintf("GitLab %s", e.Category)
}

func (e *ProviderError) Unwrap() error { return e.Err }

func (e *ProviderError) RepositoryProviderErrorCategory() string { return string(e.Category) }

func ErrorCategoryOf(err error) ErrorCategory {
	var providerError *ProviderError
	if errors.As(err, &providerError) {
		return providerError.Category
	}
	return ErrorUnreachable
}

type Client struct {
	httpClient *http.Client
	timeout    time.Duration
	now        func() time.Time
}

func (*Client) Provider() domain.RepositoryProvider { return domain.RepositoryProviderGitLab }

func (*Client) ParseRepositoryURL(raw string) (domain.RepositoryReference, error) {
	parsed, err := parseHTTPSURL(raw)
	if err != nil {
		return domain.RepositoryReference{}, err
	}
	if strings.Contains(parsed.Path, "/-/") {
		return domain.RepositoryReference{}, &ProviderError{
			Category: ErrorInvalidReference, Err: errors.New("GitLab repository URL must not contain a subpage"),
		}
	}
	repositoryPath, err := normalizeRepositoryPath(parsed.EscapedPath())
	if err != nil {
		return domain.RepositoryReference{}, err
	}
	origin := (&url.URL{Scheme: "https", Host: parsed.Host}).String()
	canonical := &url.URL{Scheme: "https", Host: parsed.Host, Path: "/" + repositoryPath}
	return domain.RepositoryReference{
		Provider: domain.RepositoryProviderGitLab, Origin: origin,
		PathWithNamespace: repositoryPath, PathLookupKey: strings.ToLower(repositoryPath),
		WebURL: canonical.String(),
	}, nil
}

func (c *Client) ParseCodeChangeURL(raw string) (domain.CodeChangeReference, error) {
	parsed, err := parseHTTPSURL(raw)
	if err != nil {
		return domain.CodeChangeReference{}, err
	}
	const marker = "/-/merge_requests/"
	markerIndex := strings.LastIndex(parsed.Path, marker)
	if markerIndex <= 0 {
		return domain.CodeChangeReference{}, &ProviderError{
			Category: ErrorInvalidReference, Err: errors.New("GitLab merge request URL is invalid"),
		}
	}
	numberText := parsed.Path[markerIndex+len(marker):]
	if numberText == "" || strings.Contains(numberText, "/") {
		return domain.CodeChangeReference{}, &ProviderError{
			Category: ErrorInvalidReference, Err: errors.New("GitLab merge request URL is invalid"),
		}
	}
	number, err := strconv.ParseInt(numberText, 10, 64)
	if err != nil || number < 1 {
		return domain.CodeChangeReference{}, &ProviderError{
			Category: ErrorInvalidReference, Err: errors.New("GitLab merge request IID is invalid"),
		}
	}
	repository, err := c.ParseRepositoryURL((&url.URL{
		Scheme: "https", Host: parsed.Host, Path: parsed.Path[:markerIndex],
	}).String())
	if err != nil {
		return domain.CodeChangeReference{}, err
	}
	canonical := &url.URL{
		Scheme: "https", Host: parsed.Host,
		Path: "/" + repository.PathWithNamespace + marker + strconv.FormatInt(number, 10),
	}
	return domain.CodeChangeReference{
		Repository: repository, Kind: domain.CodeChangeKindMergeRequest,
		ChangeNumber: number, WebURL: canonical.String(),
	}, nil
}

func NewClient(httpClient *http.Client, timeout time.Duration) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	owned := *httpClient
	previousRedirect := owned.CheckRedirect
	owned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("GitLab redirect limit exceeded")
		}
		if len(via) > 0 && !sameOrigin(request.URL, via[0].URL) {
			return errors.New("GitLab redirect changed origin")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{httpClient: &owned, timeout: timeout, now: time.Now}
}

func (c *Client) ResolveRepository(
	ctx context.Context,
	reference domain.RepositoryReference,
	credential []byte,
	requestID string,
) (domain.RepositoryIdentity, error) {
	if reference.Provider != domain.RepositoryProviderGitLab {
		return domain.RepositoryIdentity{}, &ProviderError{Category: ErrorInvalidReference, Err: errors.New("repository provider is not GitLab")}
	}
	endpoint, err := projectEndpoint(reference.Origin, reference.PathWithNamespace)
	if err != nil {
		return domain.RepositoryIdentity{}, err
	}
	var response projectResponse
	if err := c.getJSON(ctx, endpoint, credential, requestID, "resolve_project", 0, 0, &response); err != nil {
		return domain.RepositoryIdentity{}, err
	}
	project := domain.RepositoryIdentity{
		ProviderRepositoryID: strconv.FormatInt(response.ID, 10),
		PathWithNamespace:    response.PathWithNamespace,
		WebURL:               response.WebURL,
	}
	if response.DefaultBranch != nil {
		project.DefaultBranch = *response.DefaultBranch
	}
	if err := project.Validate(); err != nil {
		return domain.RepositoryIdentity{}, &ProviderError{
			Category: ErrorProviderRejected, Err: fmt.Errorf("validate GitLab project response: %w", err),
		}
	}
	return project, nil
}

func (c *Client) GetCodeChange(
	ctx context.Context,
	origin string,
	providerRepositoryID string,
	kind domain.CodeChangeKind,
	changeNumber int64,
	credential []byte,
	requestID string,
) (domain.CodeChange, error) {
	if kind != domain.CodeChangeKindMergeRequest {
		return domain.CodeChange{}, &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab code change kind must be merge_request")}
	}
	projectID, err := strconv.ParseInt(providerRepositoryID, 10, 64)
	if err != nil || projectID < 1 {
		return domain.CodeChange{}, &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab repository ID is invalid")}
	}
	endpoint, err := mergeRequestEndpoint(origin, projectID, changeNumber)
	if err != nil {
		return domain.CodeChange{}, err
	}
	var response mergeRequestResponse
	if err := c.getJSON(
		ctx, endpoint, credential, requestID, "get_merge_request", projectID, changeNumber, &response,
	); err != nil {
		return domain.CodeChange{}, err
	}
	observation := domain.CodeChangeObservation{
		Status:            domain.CodeChangeObservationConfirmed,
		ObservedAt:        c.now().UTC(),
		Title:             response.Title,
		State:             domain.CodeChangeState(response.State),
		Draft:             response.Draft,
		SourceBranch:      response.SourceBranch,
		TargetBranch:      response.TargetBranch,
		HeadSHA:           response.SHA,
		MergeCommitSHA:    response.MergeCommitSHA,
		MergedAt:          response.MergedAt,
		ProviderUpdatedAt: response.UpdatedAt,
	}
	codeChange := domain.CodeChange{
		Provider: domain.RepositoryProviderGitLab, Kind: domain.CodeChangeKindMergeRequest,
		ProviderChangeID: strconv.FormatInt(response.ID, 10), ChangeNumber: response.IID,
		WebURL: response.WebURL, Observation: observation,
	}
	if codeChange.ChangeNumber != changeNumber {
		return domain.CodeChange{}, &ProviderError{
			Category: ErrorProviderRejected,
			Err:      fmt.Errorf("GitLab merge request response IID %d does not match %d", codeChange.ChangeNumber, changeNumber),
		}
	}
	if err := codeChange.Validate(); err != nil {
		return domain.CodeChange{}, &ProviderError{
			Category: ErrorProviderRejected, Err: fmt.Errorf("validate GitLab merge request response: %w", err),
		}
	}
	return codeChange, nil
}

func (c *Client) getJSON(
	ctx context.Context,
	endpoint string,
	credential []byte,
	requestID string,
	operation string,
	projectID int64,
	iid int64,
	destination any,
) error {
	if len(credential) == 0 {
		return &ProviderError{Category: ErrorUnauthorized, Err: errors.New("GitLab credential is empty")}
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return &ProviderError{Category: ErrorInvalidReference, Err: err}
	}
	request.Header.Set("PRIVATE-TOKEN", string(credential))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Pactline-GitLab/1")
	started := time.Now()
	response, err := c.httpClient.Do(request)
	if err != nil {
		providerError := &ProviderError{Category: ErrorUnreachable, Err: err}
		c.logResult(request, requestID, operation, projectID, iid, 0, providerError.Category, "", started)
		return providerError
	}
	defer response.Body.Close()
	providerRequestID := strings.TrimSpace(response.Header.Get("X-Request-Id"))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		providerError := &ProviderError{
			Category:          categoryForStatus(response.StatusCode),
			StatusCode:        response.StatusCode,
			ProviderRequestID: providerRequestID,
		}
		c.logResult(
			request, requestID, operation, projectID, iid, response.StatusCode,
			providerError.Category, providerRequestID, started,
		)
		return providerError
	}
	limited := io.LimitReader(response.Body, maxResponseBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		providerError := &ProviderError{Category: ErrorUnreachable, StatusCode: response.StatusCode, Err: err}
		c.logResult(
			request, requestID, operation, projectID, iid, response.StatusCode,
			providerError.Category, providerRequestID, started,
		)
		return providerError
	}
	if len(body) > maxResponseBodySize {
		providerError := &ProviderError{
			Category: ErrorProviderRejected, StatusCode: response.StatusCode,
			ProviderRequestID: providerRequestID, Err: errors.New("GitLab response exceeds size limit"),
		}
		c.logResult(
			request, requestID, operation, projectID, iid, response.StatusCode,
			providerError.Category, providerRequestID, started,
		)
		return providerError
	}
	if err := json.Unmarshal(body, destination); err != nil {
		providerError := &ProviderError{
			Category: ErrorProviderRejected, StatusCode: response.StatusCode,
			ProviderRequestID: providerRequestID, Err: errors.New("GitLab returned invalid JSON"),
		}
		c.logResult(
			request, requestID, operation, projectID, iid, response.StatusCode,
			providerError.Category, providerRequestID, started,
		)
		return providerError
	}
	c.logResult(
		request, requestID, operation, projectID, iid, response.StatusCode,
		domain.CodeChangeObservationConfirmed, providerRequestID, started,
	)
	return nil
}

func (c *Client) logResult(
	request *http.Request,
	requestID string,
	operation string,
	projectID int64,
	iid int64,
	statusCode int,
	outcome any,
	providerRequestID string,
	started time.Time,
) {
	slog.InfoContext(request.Context(), "GitLab API request completed",
		"operation", operation,
		"origin", (&url.URL{Scheme: request.URL.Scheme, Host: request.URL.Host}).String(),
		"gitlab_project_id", projectID,
		"merge_request_iid", iid,
		"status", statusCode,
		"outcome", outcome,
		"provider_request_id", providerRequestID,
		"duration_ms", time.Since(started).Milliseconds(),
		"request_id", requestID,
	)
}

func projectEndpoint(origin string, pathWithNamespace string) (string, error) {
	parsed, err := validateOrigin(origin)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(pathWithNamespace) == "" || strings.HasPrefix(pathWithNamespace, "/") {
		return "", &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab project path is invalid")}
	}
	return parsed.String() + "/api/v4/projects/" + url.PathEscape(pathWithNamespace), nil
}

func mergeRequestEndpoint(origin string, projectID int64, iid int64) (string, error) {
	parsed, err := validateOrigin(origin)
	if err != nil {
		return "", err
	}
	if projectID < 1 || iid < 1 {
		return "", &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab MR identity is invalid")}
	}
	return parsed.String() + "/api/v4/projects/" + strconv.FormatInt(projectID, 10) +
		"/merge_requests/" + strconv.FormatInt(iid, 10), nil
}

func validateOrigin(origin string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab origin is invalid")}
	}
	return parsed, nil
}

func categoryForStatus(status int) ErrorCategory {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorUnauthorized
	case http.StatusNotFound:
		return ErrorNotFound
	case http.StatusTooManyRequests:
		return ErrorUnreachable
	default:
		if status >= http.StatusInternalServerError {
			return ErrorUnreachable
		}
		return ErrorProviderRejected
	}
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, &ProviderError{Category: ErrorInvalidReference, Err: errors.New("an HTTPS GitLab URL without credentials, query, or fragment is required")}
	}
	if parsed.Hostname() == "" || net.ParseIP(parsed.Hostname()) == nil && strings.Contains(parsed.Hostname(), " ") {
		return nil, &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab URL host is invalid")}
	}
	parsed.Scheme = "https"
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	parsed.Host = hostname
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	}
	return parsed, nil
}

func normalizeRepositoryPath(escapedPath string) (string, error) {
	unescaped, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab repository path escaping is invalid")}
	}
	unescaped = strings.TrimSuffix(unescaped, "/")
	unescaped = strings.TrimSuffix(unescaped, ".git")
	unescaped = strings.TrimPrefix(unescaped, "/")
	if unescaped == "" || path.Clean(unescaped) != unescaped || strings.Contains(unescaped, "//") {
		return "", &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab repository path is invalid")}
	}
	segments := strings.Split(unescaped, "/")
	if len(segments) < 2 {
		return "", &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab repository path must include a namespace")}
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" || segment == "." || segment == ".." {
			return "", &ProviderError{Category: ErrorInvalidReference, Err: errors.New("GitLab repository path is invalid")}
		}
	}
	return unescaped, nil
}

type projectResponse struct {
	ID                int64   `json:"id"`
	PathWithNamespace string  `json:"path_with_namespace"`
	WebURL            string  `json:"web_url"`
	DefaultBranch     *string `json:"default_branch"`
}

type mergeRequestResponse struct {
	ID             int64      `json:"id"`
	IID            int64      `json:"iid"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	Draft          bool       `json:"draft"`
	SourceBranch   string     `json:"source_branch"`
	TargetBranch   string     `json:"target_branch"`
	SHA            string     `json:"sha"`
	MergeCommitSHA *string    `json:"merge_commit_sha"`
	MergedAt       *time.Time `json:"merged_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	WebURL         string     `json:"web_url"`
}
