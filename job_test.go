package dedup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeAPI struct {
	timezone         string
	entries          []Entry
	markReadCalls    [][]int64
	markReadError    error
	markReadFailCall int
	entriesStart     time.Time
	entriesEnd       time.Time
}

func (f *fakeAPI) UserTimezone(context.Context) (string, error) {
	return f.timezone, nil
}

func (f *fakeAPI) Entries(_ context.Context, start, end time.Time) ([]Entry, error) {
	f.entriesStart = start
	f.entriesEnd = end
	return f.entries, nil
}

func (f *fakeAPI) MarkRead(_ context.Context, ids []int64) error {
	copyOfIDs := append([]int64(nil), ids...)
	f.markReadCalls = append(f.markReadCalls, copyOfIDs)
	if f.markReadFailCall > 0 && len(f.markReadCalls) == f.markReadFailCall {
		return f.markReadError
	}
	return nil
}

func TestDayBoundsUsesUserLocalDate(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 16, 1, 0, 0, 0, time.UTC)
	start, end, err := DayBounds(now, "", location)
	if err != nil {
		t.Fatalf("DayBounds() error = %v", err)
	}
	if got, want := start.Format(time.RFC3339), "2026-07-15T00:00:00-03:00"; got != want {
		t.Errorf("start = %s, want %s", got, want)
	}
	if got, want := end.Format(time.RFC3339), "2026-07-16T00:00:00-03:00"; got != want {
		t.Errorf("end = %s, want %s", got, want)
	}
}

func TestDayBoundsHandlesDSTAndExplicitDate(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start, end, err := DayBounds(time.Time{}, "2026-03-08", location)
	if err != nil {
		t.Fatalf("DayBounds() error = %v", err)
	}
	if got, want := end.Sub(start), 23*time.Hour; got != want {
		t.Errorf("DST day duration = %s, want %s", got, want)
	}
}

func TestDayBoundsRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, _, err := DayBounds(time.Now(), "2026/07/16", time.UTC); err == nil {
		t.Error("DayBounds() error = nil, want invalid date error")
	}
	if _, _, err := DayBounds(time.Now(), "", nil); err == nil {
		t.Error("DayBounds() error = nil, want nil timezone error")
	}
}

func TestRunDryRun(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{
		timezone: "America/Sao_Paulo",
		entries: []Entry{
			{ID: 1, Status: "unread", URL: "https://example.com/article"},
			{ID: 2, Status: "unread", URL: "https://example.com/article?utm_source=rss"},
		},
	}
	summary, err := Run(context.Background(), api, RunOptions{Date: "2026-07-16", DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Date != "2026-07-16" || summary.Timezone != api.timezone || summary.FetchedEntries != 2 ||
		summary.DuplicateGroups != 1 || summary.CanonicalURLGroups != 1 || summary.TitlePublisher24hGroups != 0 ||
		summary.RedundantUnreadEntries != 1 || summary.ChangedEntries != 0 || !summary.DryRun || len(summary.Matches) != 1 {
		t.Errorf("Run() summary = %+v", summary)
	}
	if len(api.markReadCalls) != 0 {
		t.Errorf("MarkRead calls = %v, want none", api.markReadCalls)
	}
	if got, want := api.entriesStart.Format(time.RFC3339), "2026-07-16T00:00:00-03:00"; got != want {
		t.Errorf("entry start = %s, want %s", got, want)
	}
}

func TestRunDryRunReportsTitleMatchAuditWithoutMutation(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	api := &fakeAPI{
		timezone: "UTC",
		entries: []Entry{
			{ID: 1, Status: "unread", Title: "A sufficiently long shared article title", URL: "https://example.com/one", PublishedAt: publishedAt, Feed: &Feed{ID: 1}},
			{ID: 2, Status: "unread", Title: "A sufficiently long shared article title", URL: "https://example.com/two", PublishedAt: publishedAt.Add(time.Hour), Feed: &Feed{ID: 2}},
		},
	}

	summary, err := Run(context.Background(), api, RunOptions{Date: "2026-07-16", DryRun: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.TitlePublisher24hGroups != 1 || summary.CanonicalURLGroups != 0 || len(summary.Matches) != 1 {
		t.Fatalf("Run() summary = %+v", summary)
	}
	match := summary.Matches[0]
	if match.Reason != MatchReasonTitlePublisher24h || match.Publisher != "example.com" || match.TimeSpanSeconds != 3600 {
		t.Errorf("match = %+v", match)
	}
	if len(api.markReadCalls) != 0 {
		t.Errorf("MarkRead calls = %v, want none", api.markReadCalls)
	}
}

func TestRunBatchesUpdates(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{
		timezone: "UTC",
		entries: []Entry{
			{ID: 1, Status: "unread", URL: "https://example.com/article"},
			{ID: 2, Status: "unread", URL: "https://example.com/article"},
			{ID: 3, Status: "unread", URL: "https://example.com/article"},
		},
	}
	summary, err := Run(context.Background(), api, RunOptions{Date: "2026-07-16", UpdateBatchSize: 1})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	wantCalls := [][]int64{{2}, {3}}
	if !reflect.DeepEqual(api.markReadCalls, wantCalls) {
		t.Errorf("MarkRead calls = %v, want %v", api.markReadCalls, wantCalls)
	}
	if summary.ChangedEntries != 2 {
		t.Errorf("ChangedEntries = %d, want 2", summary.ChangedEntries)
	}
	if len(summary.Matches) != 0 {
		t.Errorf("non-dry-run Matches = %v, want omitted audit details", summary.Matches)
	}
}

func TestRunPropagatesUpdateFailure(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{
		timezone:         "UTC",
		markReadError:    errors.New("update failed"),
		markReadFailCall: 1,
		entries: []Entry{
			{ID: 1, Status: "unread", URL: "https://example.com/article"},
			{ID: 2, Status: "unread", URL: "https://example.com/article"},
		},
	}
	_, err := Run(context.Background(), api, RunOptions{Date: "2026-07-16"})
	if err == nil || !errors.Is(err, api.markReadError) {
		t.Fatalf("Run() error = %v, want wrapped update error", err)
	}
}

func TestRunReportsPartialUpdateProgress(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{
		timezone:         "UTC",
		markReadError:    errors.New("update failed"),
		markReadFailCall: 2,
		entries: []Entry{
			{ID: 1, Status: "unread", URL: "https://example.com/article"},
			{ID: 2, Status: "unread", URL: "https://example.com/article"},
			{ID: 3, Status: "unread", URL: "https://example.com/article"},
		},
	}
	summary, err := Run(context.Background(), api, RunOptions{Date: "2026-07-16", UpdateBatchSize: 1})
	if err == nil || !errors.Is(err, api.markReadError) {
		t.Fatalf("Run() error = %v, want wrapped update error", err)
	}
	if summary.ChangedEntries != 1 {
		t.Errorf("ChangedEntries = %d, want 1", summary.ChangedEntries)
	}
	if !strings.Contains(err.Error(), "after changing 1 of 2 entries") || !strings.Contains(err.Error(), "batch offset 1") {
		t.Errorf("error does not report partial progress: %q", err)
	}
}
