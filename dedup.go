package dedup

import (
	"html"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MatchReasonCanonicalURL      = "canonical_url"
	MatchReasonTitlePublisher24h = "title+publisher+24h"

	titleMatchWindow = 24 * time.Hour
	minTitleTokens   = 4
	minTitleRunes    = 20
)

var analyticsQueryParameters = map[string]struct{}{
	"_hsenc":  {},
	"_hsmi":   {},
	"dclid":   {},
	"fbclid":  {},
	"gclid":   {},
	"igshid":  {},
	"mc_cid":  {},
	"mc_eid":  {},
	"mkt_tok": {},
	"msclkid": {},
	"vero_id": {},
}

// DuplicateMatch records why a group matched and which entries a dry run
// would preserve or mark read.
type DuplicateMatch struct {
	Reason             string   `json:"reason"`
	Publisher          string   `json:"publisher,omitempty"`
	NormalizedTitle    string   `json:"normalized_title,omitempty"`
	TimeSpanSeconds    int64    `json:"time_span_seconds"`
	EntryIDs           []int64  `json:"entry_ids"`
	URLs               []string `json:"urls"`
	SurvivorID         int64    `json:"survivor_id"`
	RedundantUnreadIDs []int64  `json:"redundant_unread_ids"`
}

// Plan describes the changes necessary to remove duplicates from the unread
// workflow without deleting any Miniflux entry.
type Plan struct {
	DuplicateGroups         int
	CanonicalURLGroups      int
	TitlePublisher24hGroups int
	RedundantUnreadIDs      []int64
	Matches                 []DuplicateMatch
}

// CanonicalURL converts an article URL into a conservative duplicate identity.
func CanonicalURL(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "", false
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}

	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return "", false
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}

	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", false
	}
	for name := range query {
		lowerName := strings.ToLower(name)
		if strings.HasPrefix(lowerName, "utm_") {
			query.Del(name)
			continue
		}
		if _, isAnalytics := analyticsQueryParameters[lowerName]; isAnalytics {
			query.Del(name)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.ForceQuery = false

	return parsed.String(), true
}

// NormalizeTitle produces the exact comparison key used by the guarded title
// matcher. It decodes HTML entities, lowercases Unicode letters, removes
// punctuation, and collapses whitespace.
func NormalizeTitle(title string) string {
	title = html.UnescapeString(title)
	var normalized strings.Builder
	spacePending := false
	for _, character := range title {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if spacePending && normalized.Len() > 0 {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(unicode.ToLower(character))
			spacePending = false
			continue
		}
		spacePending = normalized.Len() > 0
	}
	return normalized.String()
}

// PublisherHost returns the conservative publisher scope for title matching.
// It treats an apex host and its www host as the same publisher but does not
// merge arbitrary subdomains.
func PublisherHost(rawURL string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return "", false
	}
	return host, true
}

// PlanDeduplication applies canonical URL matching first, then joins otherwise
// distinct URLs when their substantive normalized titles, publisher hosts, and
// publication window match across different feeds.
func PlanDeduplication(entries []Entry) Plan {
	sets := newDisjointSet(len(entries))
	canonicalByIndex := make([]string, len(entries))
	canonicalGroups := make(map[string][]int)
	for index, entry := range entries {
		canonical, ok := CanonicalURL(entry.URL)
		if !ok {
			continue
		}
		canonicalByIndex[index] = canonical
		canonicalGroups[canonical] = append(canonicalGroups[canonical], index)
	}
	for _, group := range canonicalGroups {
		unionGroup(sets, group)
	}

	titleGroups := make(map[titleKey][]int)
	for index, entry := range entries {
		if canonicalByIndex[index] == "" || entry.PublishedAt.IsZero() || entry.Feed == nil || entry.Feed.ID <= 0 {
			continue
		}
		normalizedTitle := NormalizeTitle(entry.Title)
		if !substantiveTitle(normalizedTitle) {
			continue
		}
		publisher, ok := PublisherHost(entry.URL)
		if !ok {
			continue
		}
		key := titleKey{publisher: publisher, normalizedTitle: normalizedTitle}
		titleGroups[key] = append(titleGroups[key], index)
	}

	evidence := make([]titleEvidence, 0)
	titleKeys := make([]titleKey, 0, len(titleGroups))
	for key := range titleGroups {
		titleKeys = append(titleKeys, key)
	}
	sort.Slice(titleKeys, func(i, j int) bool {
		if titleKeys[i].publisher != titleKeys[j].publisher {
			return titleKeys[i].publisher < titleKeys[j].publisher
		}
		return titleKeys[i].normalizedTitle < titleKeys[j].normalizedTitle
	})
	for _, key := range titleKeys {
		candidates := titleGroups[key]
		sort.Slice(candidates, func(i, j int) bool {
			left, right := entries[candidates[i]], entries[candidates[j]]
			if !left.PublishedAt.Equal(right.PublishedAt) {
				return left.PublishedAt.Before(right.PublishedAt)
			}
			return left.ID < right.ID
		})
		for start := 0; start < len(candidates); {
			end := start + 1
			for end < len(candidates) && entries[candidates[end]].PublishedAt.Sub(entries[candidates[start]].PublishedAt) <= titleMatchWindow {
				end++
			}
			window := candidates[start:end]
			first, second, ok := qualifyingTitlePair(entries, canonicalByIndex, window)
			if ok {
				unionGroup(sets, window)
				evidence = append(evidence, titleEvidence{first: first, second: second, key: key})
			}
			start = end
		}
	}

	components := make(map[int][]int)
	for index := range entries {
		components[sets.find(index)] = append(components[sets.find(index)], index)
	}
	evidenceByRoot := make(map[int]titleEvidence)
	for _, item := range evidence {
		root := sets.find(item.first)
		if _, exists := evidenceByRoot[root]; !exists {
			evidenceByRoot[root] = item
		}
	}

	plan := Plan{}
	for root, component := range components {
		if len(component) < 2 {
			continue
		}
		sort.Slice(component, func(i, j int) bool { return entries[component[i]].ID < entries[component[j]].ID })
		groupEntries := make([]Entry, 0, len(component))
		canonicalIdentities := make(map[string]struct{})
		for _, index := range component {
			groupEntries = append(groupEntries, entries[index])
			if canonicalByIndex[index] != "" {
				canonicalIdentities[canonicalByIndex[index]] = struct{}{}
			}
		}

		survivor := preferredEntry(groupEntries)
		match := DuplicateMatch{
			Reason:          MatchReasonCanonicalURL,
			TimeSpanSeconds: publicationSpan(groupEntries) / int64(time.Second),
			EntryIDs:        make([]int64, 0, len(groupEntries)),
			URLs:            make([]string, 0, len(groupEntries)),
			SurvivorID:      survivor.ID,
		}
		if len(canonicalIdentities) > 1 {
			match.Reason = MatchReasonTitlePublisher24h
			item := evidenceByRoot[root]
			match.Publisher = item.key.publisher
			match.NormalizedTitle = item.key.normalizedTitle
			plan.TitlePublisher24hGroups++
		} else {
			plan.CanonicalURLGroups++
		}
		for _, entry := range groupEntries {
			match.EntryIDs = append(match.EntryIDs, entry.ID)
			match.URLs = append(match.URLs, entry.URL)
			if entry.ID != survivor.ID && entry.Status == "unread" && !entry.Starred {
				match.RedundantUnreadIDs = append(match.RedundantUnreadIDs, entry.ID)
				plan.RedundantUnreadIDs = append(plan.RedundantUnreadIDs, entry.ID)
			}
		}
		plan.Matches = append(plan.Matches, match)
	}

	plan.DuplicateGroups = len(plan.Matches)
	sort.Slice(plan.Matches, func(i, j int) bool { return plan.Matches[i].EntryIDs[0] < plan.Matches[j].EntryIDs[0] })
	sort.Slice(plan.RedundantUnreadIDs, func(i, j int) bool {
		return plan.RedundantUnreadIDs[i] < plan.RedundantUnreadIDs[j]
	})
	return plan
}

type titleKey struct {
	publisher       string
	normalizedTitle string
}

type titleEvidence struct {
	first  int
	second int
	key    titleKey
}

func substantiveTitle(normalized string) bool {
	return utf8.RuneCountInString(normalized) >= minTitleRunes && len(strings.Fields(normalized)) >= minTitleTokens
}

func qualifyingTitlePair(entries []Entry, canonicalByIndex []string, candidates []int) (int, int, bool) {
	if len(candidates) < 2 {
		return 0, 0, false
	}
	first := candidates[0]
	differentFeed := -1
	differentCanonical := -1
	for _, candidate := range candidates[1:] {
		feedDiffers := entries[candidate].Feed.ID != entries[first].Feed.ID
		canonicalDiffers := canonicalByIndex[candidate] != canonicalByIndex[first]
		if feedDiffers && canonicalDiffers {
			return first, candidate, true
		}
		if feedDiffers && differentFeed == -1 {
			differentFeed = candidate
		}
		if canonicalDiffers && differentCanonical == -1 {
			differentCanonical = candidate
		}
	}
	if differentFeed != -1 && differentCanonical != -1 {
		return differentFeed, differentCanonical, true
	}
	return 0, 0, false
}

func publicationSpan(entries []Entry) int64 {
	earliest, latest := entries[0].PublishedAt, entries[0].PublishedAt
	for _, entry := range entries[1:] {
		if entry.PublishedAt.Before(earliest) {
			earliest = entry.PublishedAt
		}
		if entry.PublishedAt.After(latest) {
			latest = entry.PublishedAt
		}
	}
	return int64(latest.Sub(earliest))
}

func unionGroup(sets *disjointSet, group []int) {
	for index := 1; index < len(group); index++ {
		sets.union(group[0], group[index])
	}
}

type disjointSet struct {
	parent []int
	rank   []uint8
}

func newDisjointSet(size int) *disjointSet {
	sets := &disjointSet{parent: make([]int, size), rank: make([]uint8, size)}
	for index := range sets.parent {
		sets.parent[index] = index
	}
	return sets
}

func (sets *disjointSet) find(index int) int {
	if sets.parent[index] != index {
		sets.parent[index] = sets.find(sets.parent[index])
	}
	return sets.parent[index]
}

func (sets *disjointSet) union(left, right int) {
	leftRoot, rightRoot := sets.find(left), sets.find(right)
	if leftRoot == rightRoot {
		return
	}
	if sets.rank[leftRoot] < sets.rank[rightRoot] {
		leftRoot, rightRoot = rightRoot, leftRoot
	}
	sets.parent[rightRoot] = leftRoot
	if sets.rank[leftRoot] == sets.rank[rightRoot] {
		sets.rank[leftRoot]++
	}
}

func preferredEntry(entries []Entry) Entry {
	preferred := entries[0]
	for _, candidate := range entries[1:] {
		if outranks(candidate, preferred) {
			preferred = candidate
		}
	}
	return preferred
}

func outranks(candidate, current Entry) bool {
	if candidate.Starred != current.Starred {
		return candidate.Starred
	}
	candidateUnread := candidate.Status == "unread"
	currentUnread := current.Status == "unread"
	if candidateUnread != currentUnread {
		return candidateUnread
	}
	return candidate.ID < current.ID
}
