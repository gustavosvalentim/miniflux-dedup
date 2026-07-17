package dedup_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	dedup "github.com/gsv/miniflux-dedup"
)

func TestExecutableEndToEnd(t *testing.T) {
	var updates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Auth-Token") != "e2e-token" {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/me":
			_, _ = response.Write([]byte(`{"timezone":"UTC"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/v1/entries":
			_, _ = response.Write([]byte(`{"total":3,"entries":[` +
				`{"id":1,"status":"unread","title":"A sufficiently long shared article title","url":"https://example.com/article-one","starred":false,"published_at":"2026-07-16T01:00:00Z","feed":{"id":10,"title":"Feed One"}},` +
				`{"id":2,"status":"unread","title":"A sufficiently long shared article title","url":"https://example.com/article-two","starred":false,"published_at":"2026-07-16T02:00:00Z","feed":{"id":20,"title":"Feed Two"}},` +
				`{"id":3,"status":"unread","title":"A sufficiently long shared article title","url":"https://example.com/article-three","starred":false,"published_at":"2026-07-16T03:00:00Z","feed":{"id":30,"title":"Feed Three"}}]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/v1/entries":
			var update struct {
				EntryIDs []int64 `json:"entry_ids"`
				Status   string  `json:"status"`
			}
			if err := json.NewDecoder(request.Body).Decode(&update); err != nil {
				t.Errorf("decode update: %v", err)
			}
			if len(update.EntryIDs) != 2 || update.EntryIDs[0] != 2 || update.EntryIDs[1] != 3 || update.Status != "read" {
				t.Errorf("update = %+v", update)
			}
			updates.Add(1)
			response.WriteHeader(http.StatusNoContent)
		default:
			http.Error(response, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	binary := filepath.Join(t.TempDir(), "miniflux-dedup")
	build := exec.Command("go", "build", "-o", binary, "./cmd/miniflux-dedup")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, output)
	}

	command := exec.Command(binary, "-date", "2026-07-16")
	command.Env = withEnvironment(os.Environ(), map[string]string{
		"MINIFLUX_URL":     server.URL,
		"MINIFLUX_API_KEY": "e2e-token",
	})
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run executable: %v\n%s", err, output)
	}
	var summary dedup.Summary
	if err := json.Unmarshal(output, &summary); err != nil {
		t.Fatalf("decode summary %q: %v", output, err)
	}
	if summary.ChangedEntries != 2 || summary.RedundantUnreadEntries != 2 || summary.DuplicateGroups != 1 ||
		summary.TitlePublisher24hGroups != 1 || summary.CanonicalURLGroups != 0 {
		t.Errorf("summary = %+v", summary)
	}
	if got := updates.Load(); got != 1 {
		t.Errorf("update requests = %d, want 1", got)
	}
}

func withEnvironment(current []string, replacements map[string]string) []string {
	result := make([]string, 0, len(current)+len(replacements))
	for _, item := range current {
		name, _, _ := strings.Cut(item, "=")
		if _, replace := replacements[name]; !replace {
			result = append(result, item)
		}
	}
	for name, value := range replacements {
		result = append(result, name+"="+value)
	}
	return result
}
