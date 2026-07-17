package dedup

import (
	"context"
	"fmt"
	"time"
)

const defaultUpdateBatchSize = 1000

// RunOptions controls one deduplication run.
type RunOptions struct {
	Date            string
	DryRun          bool
	Now             time.Time
	UpdateBatchSize int
}

// Summary contains the operational result of a successful run.
type Summary struct {
	Date                    string           `json:"date"`
	Timezone                string           `json:"timezone"`
	FetchedEntries          int              `json:"fetched_entries"`
	DuplicateGroups         int              `json:"duplicate_groups"`
	CanonicalURLGroups      int              `json:"canonical_url_groups"`
	TitlePublisher24hGroups int              `json:"title_publisher_24h_groups"`
	RedundantUnreadEntries  int              `json:"redundant_unread_entries"`
	ChangedEntries          int              `json:"changed_entries"`
	DryRun                  bool             `json:"dry_run"`
	Matches                 []DuplicateMatch `json:"matches,omitempty"`
}

// Run fetches one user-local day, plans deduplication, and marks redundant
// unread entries read unless dry-run mode is enabled.
func Run(ctx context.Context, api MinifluxAPI, options RunOptions) (Summary, error) {
	if api == nil {
		return Summary{}, fmt.Errorf("miniflux API is required")
	}

	timezoneName, err := api.UserTimezone(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("get miniflux user timezone: %w", err)
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return Summary{}, fmt.Errorf("load miniflux user timezone %q: %w", timezoneName, err)
	}

	start, end, err := DayBounds(options.Now, options.Date, location)
	if err != nil {
		return Summary{}, err
	}
	entries, err := api.Entries(ctx, start, end)
	if err != nil {
		return Summary{}, fmt.Errorf("get miniflux entries: %w", err)
	}

	plan := PlanDeduplication(entries)
	summary := Summary{
		Date:                    start.Format(time.DateOnly),
		Timezone:                timezoneName,
		FetchedEntries:          len(entries),
		DuplicateGroups:         plan.DuplicateGroups,
		CanonicalURLGroups:      plan.CanonicalURLGroups,
		TitlePublisher24hGroups: plan.TitlePublisher24hGroups,
		RedundantUnreadEntries:  len(plan.RedundantUnreadIDs),
		DryRun:                  options.DryRun,
	}
	if options.DryRun {
		summary.Matches = plan.Matches
	}
	if options.DryRun || len(plan.RedundantUnreadIDs) == 0 {
		return summary, nil
	}

	batchSize := options.UpdateBatchSize
	if batchSize <= 0 {
		batchSize = defaultUpdateBatchSize
	}
	for offset := 0; offset < len(plan.RedundantUnreadIDs); offset += batchSize {
		endOffset := min(offset+batchSize, len(plan.RedundantUnreadIDs))
		batch := plan.RedundantUnreadIDs[offset:endOffset]
		if err := api.MarkRead(ctx, batch); err != nil {
			return summary, fmt.Errorf(
				"mark duplicate entries read after changing %d of %d entries (batch offset %d): %w",
				summary.ChangedEntries,
				len(plan.RedundantUnreadIDs),
				offset,
				err,
			)
		}
		summary.ChangedEntries += len(batch)
	}

	return summary, nil
}

// DayBounds returns midnight at the start of a selected local calendar day and
// midnight at the start of its following day.
func DayBounds(now time.Time, explicitDate string, location *time.Location) (time.Time, time.Time, error) {
	if location == nil {
		return time.Time{}, time.Time{}, fmt.Errorf("timezone is required")
	}

	var start time.Time
	if explicitDate != "" {
		parsed, err := time.ParseInLocation(time.DateOnly, explicitDate, location)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid -date %q: expected YYYY-MM-DD", explicitDate)
		}
		start = parsed
	} else {
		if now.IsZero() {
			now = time.Now()
		}
		localNow := now.In(location)
		start = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	}

	return start, start.AddDate(0, 0, 1), nil
}
