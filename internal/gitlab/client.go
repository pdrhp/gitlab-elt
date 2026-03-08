package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/time/rate"

	"github.com/pdrhp/gitlab-elt/internal/domain"
)

// Client implements domain.GitlabClient with rate limiting, retry, and pagination.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	limiter    *rate.Limiter
	maxRetries int
	metrics    domain.MetricsRecorder
	tracer     trace.Tracer
}

const (
	endpointListGroupProjects = "list_group_projects"
	endpointListIssues        = "list_issues"
	endpointListLabelEvents   = "list_label_events"
	endpointListNotes         = "list_notes"
)

// Option configures the Client.
type Option func(*Client)

// WithRateLimit sets the max requests per second.
func WithRateLimit(rps int) Option {
	return func(c *Client) {
		c.limiter = rate.NewLimiter(rate.Limit(rps), rps)
	}
}

// WithMaxRetries sets the maximum number of retry attempts for failed requests.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		c.maxRetries = n
	}
}

// WithHTTPClient sets a custom http.Client (useful for testing).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithMetrics attaches a metrics recorder to the client.
func WithMetrics(recorder domain.MetricsRecorder) Option {
	return func(c *Client) {
		c.metrics = recorder
	}
}

// WithTracer attaches an OpenTelemetry tracer to be used by the client.
func WithTracer(tr trace.Tracer) Option {
	return func(c *Client) {
		if tr == nil {
			tr = noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/internal/gitlab")
		}
		c.tracer = tr
	}
}

// NewClient creates a new GitLab API client.
func NewClient(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		limiter:    rate.NewLimiter(rate.Limit(20), 20),
		maxRetries: 3,
		tracer:     noop.NewTracerProvider().Tracer("github.com/pdrhp/gitlab-elt/internal/gitlab"),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Ensure Client satisfies the domain interface at compile time.
var _ domain.GitlabClient = (*Client)(nil)

// ListGroupProjects retrieves all projects within a GitLab group.
func (c *Client) ListGroupProjects(ctx context.Context, groupID int) ([]domain.GitlabProject, error) {
	ctx, span := c.tracer.Start(ctx, "gitlab.ListGroupProjects",
		trace.WithAttributes(attribute.Int("group.id", groupID)),
	)
	defer span.End()

	path := fmt.Sprintf("/api/v4/groups/%d/projects", groupID)
	var result []domain.GitlabProject
	err := c.getPaginated(ctx, endpointListGroupProjects, path, nil, &result)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to list group projects")
		return nil, err
	}
	span.SetAttributes(attribute.Int("projects.count", len(result)))
	return result, nil
}

// ListIssues retrieves issues from a project updated after the given time.
func (c *Client) ListIssues(ctx context.Context, projectID int, updatedAfter time.Time) ([]domain.GitlabIssue, error) {
	ctx, span := c.tracer.Start(ctx, "gitlab.ListIssues",
		trace.WithAttributes(
			attribute.Int("project.id", projectID),
			attribute.String("updated_after", updatedAfter.Format(time.RFC3339)),
		),
	)
	defer span.End()

	path := fmt.Sprintf("/api/v4/projects/%d/issues", projectID)
	params := map[string]string{
		"updated_after": updatedAfter.Format(time.RFC3339),
		"per_page":      "100",
		"sort":          "asc",
		"order_by":      "updated_at",
	}
	var result []domain.GitlabIssue
	err := c.getPaginated(ctx, endpointListIssues, path, params, &result)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to list issues")
		return nil, err
	}
	span.SetAttributes(attribute.Int("issues.count", len(result)))
	return result, nil
}

// ListLabelEvents retrieves all label events for an issue.
func (c *Client) ListLabelEvents(ctx context.Context, projectID int, issueIID int) ([]domain.GitlabLabelEvent, error) {
	ctx, span := c.tracer.Start(ctx, "gitlab.ListLabelEvents",
		trace.WithAttributes(
			attribute.Int("project.id", projectID),
			attribute.Int("issue.iid", issueIID),
		),
	)
	defer span.End()

	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/resource_label_events", projectID, issueIID)
	var result []domain.GitlabLabelEvent
	err := c.getPaginated(ctx, endpointListLabelEvents, path, nil, &result)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to list label events")
		return nil, err
	}
	span.SetAttributes(attribute.Int("label_events.count", len(result)))
	return result, nil
}

// ListNotes retrieves all notes (comments) for an issue.
func (c *Client) ListNotes(ctx context.Context, projectID int, issueIID int) ([]domain.GitlabNote, error) {
	ctx, span := c.tracer.Start(ctx, "gitlab.ListNotes",
		trace.WithAttributes(
			attribute.Int("project.id", projectID),
			attribute.Int("issue.iid", issueIID),
		),
	)
	defer span.End()

	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes", projectID, issueIID)
	params := map[string]string{
		"sort":     "asc",
		"order_by": "created_at",
	}
	var result []domain.GitlabNote
	err := c.getPaginated(ctx, endpointListNotes, path, params, &result)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to list notes")
		return nil, err
	}
	span.SetAttributes(attribute.Int("notes.count", len(result)))
	return result, nil
}

// getPaginated fetches all pages of a paginated GitLab API endpoint and appends
// results to the target slice (passed as pointer to slice).
func (c *Client) getPaginated(ctx context.Context, endpoint, path string, params map[string]string, target interface{}) error {
	page := "1"
	for {
		pageParams := make(map[string]string)
		for k, v := range params {
			pageParams[k] = v
		}
		pageParams["page"] = page
		if _, ok := pageParams["per_page"]; !ok {
			pageParams["per_page"] = "100"
		}

		body, nextPage, err := c.doGet(ctx, endpoint, path, pageParams)
		if err != nil {
			return err
		}

		if err := appendJSONSlice(body, target); err != nil {
			return fmt.Errorf("decode response for %s: %w", path, err)
		}

		if nextPage == "" {
			break
		}
		page = nextPage
	}
	return nil
}

// doGet performs a single GET request with rate limiting and retry.
// Returns the response body, the next page number (or ""), and any error.
func (c *Client) doGet(ctx context.Context, endpoint, path string, params map[string]string) ([]byte, string, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		attemptStart := time.Now()
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			slog.Warn("retrying GitLab request",
				"path", path,
				"attempt", attempt,
				"backoff", backoff,
				"error", lastErr,
			)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		}

		// Rate limit
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, "", fmt.Errorf("rate limiter: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(path, params), nil)
		if err != nil {
			return nil, "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.recordGitLabRequest(endpoint, "error", time.Since(attemptStart))
			lastErr = fmt.Errorf("http request: %w", err)
			continue
		}
		statusLabel := strconv.Itoa(resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			c.recordGitLabRequest(endpoint, statusLabel, time.Since(attemptStart))
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		if resp.StatusCode >= 500 {
			c.recordGitLabRequest(endpoint, statusLabel, time.Since(attemptStart))
			lastErr = fmt.Errorf("server error: %d %s", resp.StatusCode, string(body))
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			c.recordGitLabRequest(endpoint, statusLabel, time.Since(attemptStart))
			lastErr = fmt.Errorf("rate limited: %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode >= 400 {
			c.recordGitLabRequest(endpoint, statusLabel, time.Since(attemptStart))
			return nil, "", fmt.Errorf("client error: %d %s", resp.StatusCode, string(body))
		}

		nextPage := resp.Header.Get("x-next-page")
		c.recordGitLabRequest(endpoint, statusLabel, time.Since(attemptStart))
		return body, nextPage, nil
	}

	return nil, "", fmt.Errorf("max retries exceeded: %w", lastErr)
}

// buildURL constructs the full URL with query parameters.
func (c *Client) buildURL(path string, params map[string]string) string {
	u := c.baseURL + path
	if len(params) == 0 {
		return u
	}
	sep := "?"
	for k, v := range params {
		u += sep + k + "=" + v
		sep = "&"
	}
	return u
}

// appendJSONSlice decodes a JSON array and appends elements to the target slice.
// target must be a pointer to a slice (e.g., *[]domain.GitlabProject).
func appendJSONSlice(data []byte, target interface{}) error {
	// Decode the page into a temporary slice of the same type
	switch t := target.(type) {
	case *[]domain.GitlabProject:
		var page []domain.GitlabProject
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	case *[]domain.GitlabIssue:
		var page []domain.GitlabIssue
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	case *[]domain.GitlabLabelEvent:
		var page []domain.GitlabLabelEvent
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	case *[]domain.GitlabNote:
		var page []domain.GitlabNote
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		*t = append(*t, page...)
	default:
		return fmt.Errorf("unsupported target type %T", target)
	}
	return nil
}

func (c *Client) recordGitLabRequest(endpoint, status string, duration time.Duration) {
	if c.metrics == nil {
		return
	}
	c.metrics.RecordGitLabRequest(endpoint, status, duration)
}
