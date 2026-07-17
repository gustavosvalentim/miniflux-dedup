package dedup

import (
	"reflect"
	"testing"
	"time"
)

func TestCanonicalURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rawURL string
		want   string
		wantOK bool
	}{
		{
			name:   "normalizes presentation and analytics differences",
			rawURL: "HTTPS://Example.COM:443?b=two&a=one&utm_source=rss&FBCLID=click#section",
			want:   "https://example.com/?a=one&b=two",
			wantOK: true,
		},
		{
			name:   "preserves meaningful path query user and port",
			rawURL: "http://user:pass@EXAMPLE.com:8080/article?edition=morning",
			want:   "http://user:pass@example.com:8080/article?edition=morning",
			wantOK: true,
		},
		{
			name:   "normalizes IPv6 default port",
			rawURL: "http://[2001:db8::1]:80",
			want:   "http://[2001:db8::1]/",
			wantOK: true,
		},
		{name: "relative URL", rawURL: "/article", wantOK: false},
		{name: "non HTTP URL", rawURL: "mailto:person@example.com", wantOK: false},
		{name: "malformed query", rawURL: "https://example.com/article?a=1;b=2", wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotOK := CanonicalURL(test.rawURL)
			if got != test.want || gotOK != test.wantOK {
				t.Errorf("CanonicalURL(%q) = (%q, %t), want (%q, %t)", test.rawURL, got, gotOK, test.want, test.wantOK)
			}
		})
	}
}

func TestPlanDeduplication(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{ID: 1, Status: "unread", URL: "https://example.com/article?utm_source=feed"},
		{ID: 2, Status: "unread", URL: "https://EXAMPLE.com/article#top", Starred: true},
		{ID: 3, Status: "read", URL: "https://example.com/article", Starred: true},
		{ID: 5, Status: "unread", URL: "https://example.com/another?b=2&a=1"},
		{ID: 4, Status: "unread", URL: "https://example.com/another?a=1&b=2"},
		{ID: 6, Status: "unread", URL: "https://example.com/unique"},
		{ID: 7, Status: "unread", URL: "not a URL"},
		{ID: 8, Status: "unread", URL: "not a URL"},
	}

	got := PlanDeduplication(entries)
	if got.DuplicateGroups != 2 {
		t.Errorf("DuplicateGroups = %d, want 2", got.DuplicateGroups)
	}
	if want := []int64{1, 5}; !reflect.DeepEqual(got.RedundantUnreadIDs, want) {
		t.Errorf("RedundantUnreadIDs = %v, want %v", got.RedundantUnreadIDs, want)
	}
}

func TestPlanDeduplicationProtectsStarredReadEntry(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{ID: 10, Status: "read", URL: "https://example.com/article", Starred: true},
		{ID: 11, Status: "unread", URL: "https://example.com/article"},
	}

	got := PlanDeduplication(entries)
	if want := []int64{11}; !reflect.DeepEqual(got.RedundantUnreadIDs, want) {
		t.Errorf("RedundantUnreadIDs = %v, want %v", got.RedundantUnreadIDs, want)
	}
}

func TestNormalizeTitleAndPublisherHost(t *testing.T) {
	t.Parallel()

	if got, want := NormalizeTitle("  AWS Glue: ETL &amp; PySpark—Pipelines!  "), "aws glue etl pyspark pipelines"; got != want {
		t.Errorf("NormalizeTitle() = %q, want %q", got, want)
	}
	if got, ok := PublisherHost("HTTPS://WWW.Example.COM.:443/article"); !ok || got != "example.com" {
		t.Errorf("PublisherHost() = (%q, %t), want (%q, true)", got, ok, "example.com")
	}
	if _, ok := PublisherHost("mailto:person@example.com"); ok {
		t.Error("PublisherHost() accepted a non-HTTP URL")
	}
}

func TestPlanDeduplicationMatchesTitleAcrossPublisherFeeds(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	title := "AWS Glue ETL Design Principles for Production PySpark Pipelines"
	entries := []Entry{
		{ID: 101, Status: "unread", Title: title, URL: "https://feeds.dzone.com/link/23567/17380150/aws-glue-pyspark-pipelines", PublishedAt: publishedAt, Feed: &Feed{ID: 23567, Title: "DZone Deployment Zone"}},
		{ID: 102, Status: "unread", Title: " AWS Glue ETL: Design Principles for Production PySpark Pipelines ", URL: "https://feeds.dzone.com/link/23566/17380177/aws-glue-pyspark-pipelines", PublishedAt: publishedAt.Add(time.Hour), Feed: &Feed{ID: 23566, Title: "DZone Tools Zone"}},
		{ID: 103, Status: "unread", Title: title, URL: "https://feeds.dzone.com/link/23561/17380151/aws-glue-pyspark-pipelines", PublishedAt: publishedAt.Add(2 * time.Hour), Feed: &Feed{ID: 23561, Title: "DZone Cloud Architecture Zone"}},
	}

	got := PlanDeduplication(entries)
	if got.DuplicateGroups != 1 || got.CanonicalURLGroups != 0 || got.TitlePublisher24hGroups != 1 {
		t.Fatalf("PlanDeduplication() counts = %+v", got)
	}
	if want := []int64{102, 103}; !reflect.DeepEqual(got.RedundantUnreadIDs, want) {
		t.Errorf("RedundantUnreadIDs = %v, want %v", got.RedundantUnreadIDs, want)
	}
	if len(got.Matches) != 1 {
		t.Fatalf("len(Matches) = %d, want 1", len(got.Matches))
	}
	match := got.Matches[0]
	if match.Reason != MatchReasonTitlePublisher24h || match.Publisher != "feeds.dzone.com" ||
		match.NormalizedTitle != "aws glue etl design principles for production pyspark pipelines" ||
		match.TimeSpanSeconds != int64((2*time.Hour)/time.Second) || match.SurvivorID != 101 {
		t.Errorf("match = %+v", match)
	}
}

func TestPlanDeduplicationTitleSafeguards(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		left  Entry
		right Entry
	}{
		{
			name:  "different publisher host",
			left:  Entry{ID: 1, Title: "A sufficiently long shared article title", URL: "https://one.example/article", PublishedAt: base, Feed: &Feed{ID: 1}},
			right: Entry{ID: 2, Title: "A sufficiently long shared article title", URL: "https://two.example/article", PublishedAt: base, Feed: &Feed{ID: 2}},
		},
		{
			name:  "more than twenty four hours apart",
			left:  Entry{ID: 1, Title: "A sufficiently long shared article title", URL: "https://example.com/one", PublishedAt: base, Feed: &Feed{ID: 1}},
			right: Entry{ID: 2, Title: "A sufficiently long shared article title", URL: "https://example.com/two", PublishedAt: base.Add(24*time.Hour + time.Second), Feed: &Feed{ID: 2}},
		},
		{
			name:  "same feed",
			left:  Entry{ID: 1, Title: "A sufficiently long shared article title", URL: "https://example.com/one", PublishedAt: base, Feed: &Feed{ID: 1}},
			right: Entry{ID: 2, Title: "A sufficiently long shared article title", URL: "https://example.com/two", PublishedAt: base, Feed: &Feed{ID: 1}},
		},
		{
			name:  "short generic title",
			left:  Entry{ID: 1, Title: "Daily market update", URL: "https://example.com/one", PublishedAt: base, Feed: &Feed{ID: 1}},
			right: Entry{ID: 2, Title: "Daily market update", URL: "https://example.com/two", PublishedAt: base, Feed: &Feed{ID: 2}},
		},
		{
			name:  "missing feed metadata",
			left:  Entry{ID: 1, Title: "A sufficiently long shared article title", URL: "https://example.com/one", PublishedAt: base},
			right: Entry{ID: 2, Title: "A sufficiently long shared article title", URL: "https://example.com/two", PublishedAt: base, Feed: &Feed{ID: 2}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.left.Status = "unread"
			test.right.Status = "unread"
			got := PlanDeduplication([]Entry{test.left, test.right})
			if got.DuplicateGroups != 0 || len(got.RedundantUnreadIDs) != 0 {
				t.Errorf("PlanDeduplication() = %+v, want no title match", got)
			}
		})
	}
}

func TestPlanDeduplicationCombinesURLAndTitleIntoOneGroup(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	title := "A sufficiently long shared article title"
	entries := []Entry{
		{ID: 1, Status: "unread", Title: title, URL: "https://example.com/one", PublishedAt: publishedAt, Feed: &Feed{ID: 1}},
		{ID: 2, Status: "unread", Title: title, URL: "https://example.com/one?utm_source=rss", PublishedAt: publishedAt, Feed: &Feed{ID: 2}},
		{ID: 3, Status: "unread", Title: title, URL: "https://example.com/two", PublishedAt: publishedAt, Feed: &Feed{ID: 3}},
	}

	got := PlanDeduplication(entries)
	if got.DuplicateGroups != 1 || got.CanonicalURLGroups != 0 || got.TitlePublisher24hGroups != 1 {
		t.Fatalf("PlanDeduplication() counts = %+v", got)
	}
	if want := []int64{2, 3}; !reflect.DeepEqual(got.RedundantUnreadIDs, want) {
		t.Errorf("RedundantUnreadIDs = %v, want %v", got.RedundantUnreadIDs, want)
	}
}

func TestPlanDeduplicationNeverChangesRedundantStarredEntries(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{ID: 1, Status: "unread", URL: "https://example.com/article", Starred: true},
		{ID: 2, Status: "unread", URL: "https://example.com/article", Starred: true},
		{ID: 3, Status: "unread", URL: "https://example.com/article"},
	}

	got := PlanDeduplication(entries)
	if want := []int64{3}; !reflect.DeepEqual(got.RedundantUnreadIDs, want) {
		t.Errorf("RedundantUnreadIDs = %v, want %v", got.RedundantUnreadIDs, want)
	}
	if want := []int64{3}; !reflect.DeepEqual(got.Matches[0].RedundantUnreadIDs, want) {
		t.Errorf("match RedundantUnreadIDs = %v, want %v", got.Matches[0].RedundantUnreadIDs, want)
	}
}
