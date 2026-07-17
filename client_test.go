package dedup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientRetrievesTimezoneAndPaginatedEntriesAndMarksRead(t *testing.T) {
	t.Parallel()

	const token = "test-token"
	var receivedUpdate statusUpdateRequest
	start := time.Date(2026, 7, 16, 0, 0, 0, 0, time.FixedZone("test", -3*60*60))
	publishedAt := start.Add(time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("X-Auth-Token"); got != token {
			t.Errorf("X-Auth-Token = %q, want %q", got, token)
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/base/v1/me":
			writeJSON(t, response, userResponse{Timezone: "America/Sao_Paulo"})
		case request.Method == http.MethodGet && request.URL.Path == "/base/v1/entries":
			assertEntryQuery(t, request.URL.Query())
			afterEntryID := request.URL.Query().Get("after_entry_id")
			if afterEntryID == "" {
				entries := make([]Entry, entriesPageSize)
				for index := range entries {
					entries[index] = Entry{
						ID:          int64(index + 1),
						Status:      "unread",
						URL:         fmt.Sprintf("https://example.com/%d", index+1),
						PublishedAt: publishedAt,
					}
				}
				writeJSON(t, response, entriesResponse{Total: entriesPageSize + 1, Entries: entries})
				return
			}
			if afterEntryID != strconv.Itoa(entriesPageSize) {
				t.Errorf("after_entry_id = %q, want %d", afterEntryID, entriesPageSize)
			}
			writeJSON(t, response, entriesResponse{
				Total: 1,
				Entries: []Entry{{
					ID:          entriesPageSize + 1,
					Status:      "read",
					URL:         "https://example.com/last",
					PublishedAt: publishedAt,
				}},
			})
		case request.Method == http.MethodPut && request.URL.Path == "/base/v1/entries":
			if err := json.NewDecoder(request.Body).Decode(&receivedUpdate); err != nil {
				t.Errorf("decode update: %v", err)
			}
			response.WriteHeader(http.StatusNoContent)
		default:
			http.Error(response, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL + "/base")
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(t, Config{BaseURL: baseURL, APIToken: token}, server.Client())
	timezone, err := client.UserTimezone(context.Background())
	if err != nil {
		t.Fatalf("UserTimezone() error = %v", err)
	}
	if timezone != "America/Sao_Paulo" {
		t.Errorf("timezone = %q", timezone)
	}
	entries, err := client.Entries(context.Background(), start, start.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if got, want := len(entries), entriesPageSize+1; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	if err := client.MarkRead(context.Background(), []int64{2, 3}); err != nil {
		t.Fatalf("MarkRead() error = %v", err)
	}
	if receivedUpdate.Status != "read" || !reflect.DeepEqual(receivedUpdate.EntryIDs, []int64{2, 3}) {
		t.Errorf("update = %+v", receivedUpdate)
	}
}

func TestClientDetectsPaginationWithoutProgress(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, entriesResponse{
			Total: 2,
			Entries: []Entry{{
				ID:          1,
				Status:      "unread",
				URL:         "https://example.com/article",
				PublishedAt: time.Unix(150, 0),
			}},
		})
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	client := newTestClient(t, Config{BaseURL: baseURL, APIToken: "token"}, server.Client())
	_, err := client.Entries(context.Background(), time.Unix(100, 0), time.Unix(200, 0))
	if err == nil || !strings.Contains(err.Error(), "made no progress") {
		t.Fatalf("Entries() error = %v, want pagination error", err)
	}
}

func TestClientRedactsTokenFromAPIError(t *testing.T) {
	t.Parallel()

	const token = "never-print-this"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "rejected token "+token, http.StatusUnauthorized)
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	client := newTestClient(t, Config{BaseURL: baseURL, APIToken: token}, server.Client())
	_, err := client.UserTimezone(context.Background())
	if err == nil {
		t.Fatal("UserTimezone() error = nil, want error")
	}
	if strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error did not redact token: %q", err)
	}
}

func TestClientFiltersFractionalSecondBeforeDay(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, entriesResponse{
			Total: 2,
			Entries: []Entry{
				{ID: 1, Status: "unread", URL: "https://example.com/previous", PublishedAt: start.Add(-500 * time.Millisecond)},
				{ID: 2, Status: "unread", URL: "https://example.com/current", PublishedAt: start},
			},
		})
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	client := newTestClient(t, Config{BaseURL: baseURL, APIToken: "token"}, server.Client())
	entries, err := client.Entries(context.Background(), start, start.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != 2 {
		t.Errorf("entries = %+v, want only ID 2", entries)
	}
}

func TestClientRejectsRedirectWithoutForwardingToken(t *testing.T) {
	t.Parallel()

	var targetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		targetCalled.Store(true)
		if request.Header.Get("X-Auth-Token") != "" {
			t.Errorf("redirect target received X-Auth-Token")
		}
		writeJSON(t, response, userResponse{Timezone: "UTC"})
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+"/v1/me", http.StatusFound)
	}))
	defer source.Close()

	baseURL, _ := url.Parse(source.URL)
	client := newTestClient(t, Config{BaseURL: baseURL, APIToken: "token"}, source.Client())
	_, err := client.UserTimezone(context.Background())
	if err == nil || !strings.Contains(err.Error(), "redirects are not allowed") {
		t.Fatalf("UserTimezone() error = %v, want redirect rejection", err)
	}
	if targetCalled.Load() {
		t.Error("redirect target was called")
	}
}

func TestClientRejectsOversizedSuccessfulResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, userResponse{Timezone: strings.Repeat("x", 128)})
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	client := newTestClient(t, Config{BaseURL: baseURL, APIToken: "token"}, server.Client())
	client.maxResponseBodySize = 32
	_, err := client.UserTimezone(context.Background())
	if err == nil || !strings.Contains(err.Error(), "32-byte safety limit") {
		t.Fatalf("UserTimezone() error = %v, want response-size error", err)
	}
}

func TestClientRejectsImplausibleEntryCount(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, entriesResponse{Total: 2, Entries: []Entry{}})
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	client := newTestClient(t, Config{BaseURL: baseURL, APIToken: "token"}, server.Client())
	client.maxEntriesPerRun = 1
	_, err := client.Entries(context.Background(), time.Unix(100, 0), time.Unix(200, 0))
	if err == nil || !strings.Contains(err.Error(), "safety limit of 1 entries") {
		t.Fatalf("Entries() error = %v, want entry-count error", err)
	}
}

func TestNewClientRejectsZeroConfig(t *testing.T) {
	t.Parallel()

	if _, err := NewClient(Config{}, nil); err == nil {
		t.Fatal("NewClient() error = nil, want configuration error")
	}
}

func assertEntryQuery(t *testing.T, query url.Values) {
	t.Helper()
	if got, want := query["status"], []string{"read", "unread"}; !reflect.DeepEqual(got, want) {
		t.Errorf("status = %v, want %v", got, want)
	}
	checks := map[string]string{
		"limit":            strconv.Itoa(entriesPageSize),
		"order":            "id",
		"direction":        "asc",
		"published_after":  "1784170799",
		"published_before": "1784257200",
	}
	for name, want := range checks {
		if got := query.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if _, exists := query["offset"]; exists {
		t.Errorf("offset pagination was used: %v", query["offset"])
	}
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func newTestClient(t *testing.T, config Config, httpClient *http.Client) *Client {
	t.Helper()
	client, err := NewClient(config, httpClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}
