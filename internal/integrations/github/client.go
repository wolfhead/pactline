package github

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
	githubAPIOrigin     = "https://api.github.com"
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
	RateLimited       bool
	RetryAfter        string
	Err               error
}

func (e *ProviderError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("GitHub %s (status %d)", e.Category, e.StatusCode)
	}
	return fmt.Sprintf("GitHub %s", e.Category)
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

func (*Client) Provider() domain.RepositoryProvider { return domain.RepositoryProviderGitHub }

func (*Client) ParseRepositoryURL(raw string) (domain.RepositoryReference, error) {
	parsed, err := parseHTTPSURL(raw)
	if err != nil {
		return domain.RepositoryReference{}, err
	}
	repositoryPath, err := normalizeRepositoryPath(parsed.EscapedPath())
	if err != nil {
		return domain.RepositoryReference{}, err
	}
	origin := (&url.URL{Scheme: "https", Host: parsed.Host}).String()
	canonical := &url.URL{Scheme: "https", Host: parsed.Host, Path: "/" + repositoryPath}
	return domain.RepositoryReference{
		Provider: domain.RepositoryProviderGitHub, Origin: origin,
		PathWithNamespace: repositoryPath, PathLookupKey: strings.ToLower(repositoryPath),
		WebURL: canonical.String(),
	}, nil
}

func (c *Client) ParseCodeChangeURL(raw string) (domain.CodeChangeReference, error) {
	parsed, err := parseHTTPSURL(raw)
	if err != nil {
		return domain.CodeChangeReference{}, err
	}
	segments, err := decodedPathSegments(parsed.EscapedPath())
	if err != nil || len(segments) != 4 || segments[2] != "pull" {
		return domain.CodeChangeReference{}, invalidReference("GitHub pull request URL is invalid")
	}
	number, err := strconv.ParseInt(segments[3], 10, 64)
	if err != nil || number < 1 {
		return domain.CodeChangeReference{}, invalidReference("GitHub pull request number is invalid")
	}
	repository, err := c.ParseRepositoryURL((&url.URL{
		Scheme: "https", Host: parsed.Host, Path: "/" + segments[0] + "/" + segments[1],
	}).String())
	if err != nil {
		return domain.CodeChangeReference{}, err
	}
	canonical := repository.WebURL + "/pull/" + strconv.FormatInt(number, 10)
	return domain.CodeChangeReference{
		Repository: repository, Kind: domain.CodeChangeKindPullRequest,
		ChangeNumber: number, WebURL: canonical,
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
			return errors.New("GitHub redirect limit exceeded")
		}
		if len(via) > 0 && !sameOrigin(request.URL, via[0].URL) {
			return errors.New("GitHub redirect changed origin")
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
	if reference.Provider != domain.RepositoryProviderGitHub {
		return domain.RepositoryIdentity{}, invalidReference("repository provider is not GitHub")
	}
	endpoint, err := repositoryEndpoint(reference)
	if err != nil {
		return domain.RepositoryIdentity{}, err
	}
	var response repositoryResponse
	if err := c.getJSON(ctx, endpoint, credential, requestID, "resolve_repository", 0, 0, &response); err != nil {
		return domain.RepositoryIdentity{}, err
	}
	repository := domain.RepositoryIdentity{
		ProviderRepositoryID: strconv.FormatInt(response.ID, 10),
		PathWithNamespace:    response.FullName, WebURL: response.HTMLURL,
		DefaultBranch: response.DefaultBranch,
	}
	if err := repository.Validate(); err != nil {
		return domain.RepositoryIdentity{}, rejected(fmt.Errorf("validate GitHub repository response: %w", err))
	}
	return repository, nil
}

func (c *Client) GetCodeChange(
	ctx context.Context,
	reference domain.RepositoryReference,
	providerRepositoryID string,
	kind domain.CodeChangeKind,
	changeNumber int64,
	credential []byte,
	requestID string,
) (domain.CodeChange, error) {
	if reference.Provider != domain.RepositoryProviderGitHub || kind != domain.CodeChangeKindPullRequest {
		return domain.CodeChange{}, invalidReference("GitHub code change kind must be pull_request")
	}
	repositoryID, err := strconv.ParseInt(providerRepositoryID, 10, 64)
	if err != nil || repositoryID < 1 || changeNumber < 1 {
		return domain.CodeChange{}, invalidReference("GitHub repository or pull request identity is invalid")
	}
	endpoint, err := pullRequestEndpoint(reference, changeNumber)
	if err != nil {
		return domain.CodeChange{}, err
	}
	var response pullRequestResponse
	if err := c.getJSON(ctx, endpoint, credential, requestID, "get_pull_request", repositoryID, changeNumber, &response); err != nil {
		return domain.CodeChange{}, err
	}
	if response.Base.Repository.ID != repositoryID || !strings.EqualFold(response.Base.Repository.FullName, reference.PathWithNamespace) {
		return domain.CodeChange{}, rejected(errors.New("GitHub pull request response belongs to a different repository"))
	}
	state, err := codeChangeState(response.State, response.MergedAt)
	if err != nil {
		return domain.CodeChange{}, err
	}
	observation := domain.CodeChangeObservation{
		Status: domain.CodeChangeObservationConfirmed, ObservedAt: c.now().UTC(),
		Title: response.Title, State: state, Draft: response.Draft,
		SourceBranch: response.Head.Ref, TargetBranch: response.Base.Ref, HeadSHA: response.Head.SHA,
		MergeCommitSHA: response.MergeCommitSHA, MergedAt: response.MergedAt,
		ProviderUpdatedAt: response.UpdatedAt,
	}
	codeChange := domain.CodeChange{
		Provider: domain.RepositoryProviderGitHub, Kind: domain.CodeChangeKindPullRequest,
		ProviderChangeID: strconv.FormatInt(response.ID, 10), ChangeNumber: response.Number,
		WebURL: response.HTMLURL, Observation: observation,
	}
	if response.Number != changeNumber {
		return domain.CodeChange{}, rejected(fmt.Errorf("GitHub pull request response number %d does not match %d", response.Number, changeNumber))
	}
	if err := codeChange.Validate(); err != nil {
		return domain.CodeChange{}, rejected(fmt.Errorf("validate GitHub pull request response: %w", err))
	}
	return codeChange, nil
}

func (c *Client) getJSON(
	ctx context.Context,
	endpoint string,
	credential []byte,
	requestID string,
	operation string,
	repositoryID int64,
	pullRequestNumber int64,
	destination any,
) error {
	if len(credential) == 0 {
		return &ProviderError{Category: ErrorUnauthorized, Err: errors.New("GitHub credential is empty")}
	}
	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return invalidReference("GitHub API endpoint is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+string(credential))
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "Pactline/1.0")
	started := time.Now()
	response, err := c.httpClient.Do(request)
	if err != nil {
		providerError := &ProviderError{Category: ErrorUnreachable, Err: err}
		c.logResult(request, requestID, operation, repositoryID, pullRequestNumber, 0, providerError.Category, "", started)
		return providerError
	}
	defer response.Body.Close()
	providerRequestID := strings.TrimSpace(response.Header.Get("X-GitHub-Request-Id"))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		rateLimited := response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode == http.StatusForbidden && (strings.TrimSpace(response.Header.Get("Retry-After")) != "" || strings.TrimSpace(response.Header.Get("X-RateLimit-Remaining")) == "0")
		providerError := &ProviderError{
			Category: categoryForResponse(response), StatusCode: response.StatusCode,
			ProviderRequestID: providerRequestID, RateLimited: rateLimited,
			RetryAfter: strings.TrimSpace(response.Header.Get("Retry-After")),
		}
		c.logResult(request, requestID, operation, repositoryID, pullRequestNumber, response.StatusCode, providerError.Category, providerRequestID, started)
		return providerError
	}
	limited := io.LimitReader(response.Body, maxResponseBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		providerError := &ProviderError{Category: ErrorUnreachable, StatusCode: response.StatusCode, ProviderRequestID: providerRequestID, Err: err}
		c.logResult(request, requestID, operation, repositoryID, pullRequestNumber, response.StatusCode, providerError.Category, providerRequestID, started)
		return providerError
	}
	if len(body) > maxResponseBodySize {
		providerError := &ProviderError{Category: ErrorProviderRejected, StatusCode: response.StatusCode, ProviderRequestID: providerRequestID, Err: errors.New("GitHub response exceeds size limit")}
		c.logResult(request, requestID, operation, repositoryID, pullRequestNumber, response.StatusCode, providerError.Category, providerRequestID, started)
		return providerError
	}
	if err := json.Unmarshal(body, destination); err != nil {
		providerError := &ProviderError{Category: ErrorProviderRejected, StatusCode: response.StatusCode, ProviderRequestID: providerRequestID, Err: errors.New("GitHub returned invalid JSON")}
		c.logResult(request, requestID, operation, repositoryID, pullRequestNumber, response.StatusCode, providerError.Category, providerRequestID, started)
		return providerError
	}
	c.logResult(request, requestID, operation, repositoryID, pullRequestNumber, response.StatusCode, domain.CodeChangeObservationConfirmed, providerRequestID, started)
	return nil
}

func (c *Client) logResult(
	request *http.Request,
	requestID string,
	operation string,
	repositoryID int64,
	pullRequestNumber int64,
	statusCode int,
	outcome any,
	providerRequestID string,
	started time.Time,
) {
	slog.InfoContext(request.Context(), "GitHub API request completed",
		"operation", operation,
		"method", request.Method,
		"route", githubRouteTemplate(operation),
		"origin", (&url.URL{Scheme: request.URL.Scheme, Host: request.URL.Host}).String(),
		"github_repository_id", repositoryID,
		"pull_request_number", pullRequestNumber,
		"status", statusCode,
		"outcome", outcome,
		"provider_request_id", providerRequestID,
		"duration_ms", time.Since(started).Milliseconds(),
		"request_id", requestID,
	)
}

func githubRouteTemplate(operation string) string {
	if operation == "get_pull_request" {
		return "/repos/{owner}/{repo}/pulls/{number}"
	}
	return "/repos/{owner}/{repo}"
}

func repositoryEndpoint(reference domain.RepositoryReference) (string, error) {
	base, err := apiBase(reference.Origin)
	if err != nil {
		return "", err
	}
	segments := strings.Split(reference.PathWithNamespace, "/")
	if len(segments) != 2 {
		return "", invalidReference("GitHub repository path must contain owner and repository")
	}
	return base + "/repos/" + url.PathEscape(segments[0]) + "/" + url.PathEscape(segments[1]), nil
}

func pullRequestEndpoint(reference domain.RepositoryReference, number int64) (string, error) {
	endpoint, err := repositoryEndpoint(reference)
	if err != nil {
		return "", err
	}
	if number < 1 {
		return "", invalidReference("GitHub pull request number is invalid")
	}
	return endpoint + "/pulls/" + strconv.FormatInt(number, 10), nil
}

func apiBase(origin string) (string, error) {
	parsed, err := validateOrigin(origin)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(parsed.Host, "github.com") {
		return githubAPIOrigin, nil
	}
	return parsed.String() + "/api/v3", nil
}

func validateOrigin(origin string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, invalidReference("GitHub origin is invalid")
	}
	return parsed, nil
}

func categoryForResponse(response *http.Response) ErrorCategory {
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return ErrorUnauthorized
	case http.StatusNotFound:
		return ErrorNotFound
	case http.StatusTooManyRequests:
		return ErrorUnreachable
	case http.StatusForbidden:
		if strings.TrimSpace(response.Header.Get("Retry-After")) != "" ||
			strings.TrimSpace(response.Header.Get("X-RateLimit-Remaining")) == "0" {
			return ErrorUnreachable
		}
		return ErrorUnauthorized
	default:
		if response.StatusCode >= http.StatusInternalServerError {
			return ErrorUnreachable
		}
		return ErrorProviderRejected
	}
}

func codeChangeState(state string, mergedAt *time.Time) (domain.CodeChangeState, error) {
	if mergedAt != nil {
		return domain.CodeChangeStateMerged, nil
	}
	switch state {
	case "open":
		return domain.CodeChangeStateOpened, nil
	case "closed":
		return domain.CodeChangeStateClosed, nil
	default:
		return "", rejected(fmt.Errorf("GitHub pull request state %q is invalid", state))
	}
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, invalidReference("an HTTPS GitHub URL without credentials, query, or fragment is required")
	}
	if parsed.Hostname() == "" || net.ParseIP(parsed.Hostname()) == nil && strings.Contains(parsed.Hostname(), " ") {
		return nil, invalidReference("GitHub URL host is invalid")
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
	segments, err := decodedPathSegments(escapedPath)
	if err != nil {
		return "", err
	}
	if len(segments) != 2 {
		return "", invalidReference("GitHub repository path must contain exactly owner and repository")
	}
	segments[1] = strings.TrimSuffix(segments[1], ".git")
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" || segment == "." || segment == ".." {
			return "", invalidReference("GitHub repository path is invalid")
		}
	}
	return strings.Join(segments, "/"), nil
}

func decodedPathSegments(escapedPath string) ([]string, error) {
	unescaped, err := url.PathUnescape(escapedPath)
	if err != nil {
		return nil, invalidReference("GitHub path escaping is invalid")
	}
	unescaped = strings.TrimSuffix(unescaped, "/")
	unescaped = strings.TrimPrefix(unescaped, "/")
	if unescaped == "" || path.Clean(unescaped) != unescaped || strings.Contains(unescaped, "//") {
		return nil, invalidReference("GitHub path is invalid")
	}
	segments := strings.Split(unescaped, "/")
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" || segment == "." || segment == ".." {
			return nil, invalidReference("GitHub path is invalid")
		}
	}
	return segments, nil
}

func invalidReference(message string) *ProviderError {
	return &ProviderError{Category: ErrorInvalidReference, Err: errors.New(message)}
}

func rejected(err error) *ProviderError {
	return &ProviderError{Category: ErrorProviderRejected, Err: err}
}

type repositoryResponse struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}

type pullRequestResponse struct {
	ID             int64      `json:"id"`
	Number         int64      `json:"number"`
	HTMLURL        string     `json:"html_url"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	Draft          bool       `json:"draft"`
	MergeCommitSHA *string    `json:"merge_commit_sha"`
	MergedAt       *time.Time `json:"merged_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Head           struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		Ref        string `json:"ref"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"base"`
}
