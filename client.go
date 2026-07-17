package dedup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	entriesPageSize            = 1000
	errorBodyLimit             = 4096
	defaultMaxResponseBodySize = 64 << 20
	defaultMaxEntriesPerRun    = 100_000
)

// Entry is the subset of a Miniflux entry needed for deduplication.
type Entry struct {
	ID          int64     `json:"id"`
	Status      string    `json:"status"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Starred     bool      `json:"starred"`
	PublishedAt time.Time `json:"published_at"`
	Feed        *Feed     `json:"feed,omitempty"`
}

// Feed is the subset of a Miniflux feed used for cross-feed title matching and
// displayed in dry-run reports.
type Feed struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type userResponse struct {
	Timezone string `json:"timezone"`
}

type entriesResponse struct {
	Total   int     `json:"total"`
	Entries []Entry `json:"entries"`
}

type statusUpdateRequest struct {
	EntryIDs []int64 `json:"entry_ids"`
	Status   string  `json:"status"`
}

// MinifluxAPI describes the API operations required by a deduplication run.
type MinifluxAPI interface {
	UserTimezone(context.Context) (string, error)
	Entries(context.Context, time.Time, time.Time) ([]Entry, error)
	MarkRead(context.Context, []int64) error
}

// Client is a small Miniflux REST API client.
type Client struct {
	baseURL             string
	apiToken            string
	httpClient          *http.Client
	maxResponseBodySize int64
	maxEntriesPerRun    int
}

// NewClient returns a client backed by the provided HTTP client.
func NewClient(config Config, httpClient *http.Client) (*Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	safeHTTPClient := *httpClient
	safeHTTPClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("miniflux API redirects are not allowed")
	}

	return &Client{
		baseURL:             strings.TrimSuffix(config.BaseURL.String(), "/"),
		apiToken:            config.APIToken,
		httpClient:          &safeHTTPClient,
		maxResponseBodySize: defaultMaxResponseBodySize,
		maxEntriesPerRun:    defaultMaxEntriesPerRun,
	}, nil
}

// UserTimezone returns the authenticated user's configured IANA timezone.
func (c *Client) UserTimezone(ctx context.Context) (string, error) {
	var user userResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/me", nil, &user, http.StatusOK); err != nil {
		return "", err
	}
	if strings.TrimSpace(user.Timezone) == "" {
		return "", errors.New("miniflux user has an empty timezone")
	}
	return user.Timezone, nil
}

// Entries retrieves all read and unread entries within the half-open interval.
func (c *Client) Entries(ctx context.Context, start, end time.Time) ([]Entry, error) {
	entries := make([]Entry, 0)
	afterEntryID := int64(0)
	processedEntries := 0

	for {
		query := url.Values{}
		query.Add("status", "read")
		query.Add("status", "unread")
		query.Set("limit", strconv.Itoa(entriesPageSize))
		query.Set("order", "id")
		query.Set("direction", "asc")
		query.Set("published_after", strconv.FormatInt(start.Add(-time.Second).Unix(), 10))
		query.Set("published_before", strconv.FormatInt(end.Unix(), 10))
		if afterEntryID > 0 {
			query.Set("after_entry_id", strconv.FormatInt(afterEntryID, 10))
		}

		var page entriesResponse
		path := "/v1/entries?" + query.Encode()
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &page, http.StatusOK); err != nil {
			return nil, err
		}
		if page.Total < 0 || len(page.Entries) > page.Total {
			return nil, errors.New("miniflux entries response contains an invalid total")
		}
		if page.Total > c.maxEntriesPerRun || processedEntries+len(page.Entries) > c.maxEntriesPerRun {
			return nil, fmt.Errorf("miniflux entries response exceeds the safety limit of %d entries", c.maxEntriesPerRun)
		}
		if len(page.Entries) == 0 {
			if page.Total == 0 {
				return entries, nil
			}
			return nil, fmt.Errorf("miniflux entries pagination made no progress after entry ID %d", afterEntryID)
		}

		for _, entry := range page.Entries {
			if entry.ID <= afterEntryID {
				return nil, fmt.Errorf(
					"miniflux entries pagination made no progress: returned entry ID %d after %d",
					entry.ID,
					afterEntryID,
				)
			}
			afterEntryID = entry.ID
			processedEntries++
			if entry.PublishedAt.IsZero() {
				return nil, fmt.Errorf("miniflux entry %d has no published_at timestamp", entry.ID)
			}
			if !entry.PublishedAt.Before(start) && entry.PublishedAt.Before(end) {
				entries = append(entries, entry)
			}
		}
		if len(page.Entries) >= page.Total {
			return entries, nil
		}
	}
}

// MarkRead updates the supplied entries to read status.
func (c *Client) MarkRead(ctx context.Context, entryIDs []int64) error {
	if len(entryIDs) == 0 {
		return nil
	}

	body := statusUpdateRequest{EntryIDs: entryIDs, Status: "read"}
	return c.doJSON(ctx, http.MethodPut, "/v1/entries", body, nil, http.StatusNoContent)
}

func (c *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	body any,
	destination any,
	expectedStatus int,
) error {
	var requestBody io.Reader
	if body != nil {
		encodedBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode miniflux request: %w", err)
		}
		requestBody = bytes.NewReader(encodedBody)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return fmt.Errorf("create miniflux request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Auth-Token", c.apiToken)
	request.Header.Set("User-Agent", "miniflux-dedup/1")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("miniflux %s request failed: %w", method, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != expectedStatus {
		errorBody, readErr := io.ReadAll(io.LimitReader(response.Body, errorBodyLimit))
		if readErr != nil {
			return fmt.Errorf("miniflux %s returned HTTP %d", method, response.StatusCode)
		}
		message := strings.TrimSpace(string(errorBody))
		if c.apiToken != "" {
			message = strings.ReplaceAll(message, c.apiToken, "[redacted]")
		}
		if message == "" {
			return fmt.Errorf("miniflux %s returned HTTP %d", method, response.StatusCode)
		}
		return fmt.Errorf("miniflux %s returned HTTP %d: %s", method, response.StatusCode, message)
	}

	if destination == nil {
		bytesRead, copyErr := io.CopyN(io.Discard, response.Body, c.maxResponseBodySize+1)
		if bytesRead > c.maxResponseBodySize {
			return fmt.Errorf("miniflux %s response exceeds the %d-byte safety limit", method, c.maxResponseBodySize)
		}
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			return fmt.Errorf("read miniflux %s response: %w", method, copyErr)
		}
		return nil
	}
	limitedBody := &io.LimitedReader{R: response.Body, N: c.maxResponseBodySize + 1}
	decoder := json.NewDecoder(limitedBody)
	if err := decoder.Decode(destination); limitedBody.N == 0 {
		return fmt.Errorf("miniflux %s response exceeds the %d-byte safety limit", method, c.maxResponseBodySize)
	} else if err != nil {
		return fmt.Errorf("decode miniflux %s response: %w", method, err)
	}
	var trailingValue any
	if err := decoder.Decode(&trailingValue); limitedBody.N == 0 {
		return fmt.Errorf("miniflux %s response exceeds the %d-byte safety limit", method, c.maxResponseBodySize)
	} else if !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode miniflux %s response: unexpected trailing JSON value", method)
		}
		return fmt.Errorf("decode miniflux %s response: %w", method, err)
	}
	return nil
}
