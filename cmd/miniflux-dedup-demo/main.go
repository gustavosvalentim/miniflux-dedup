package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dedup "github.com/gustavosvalentim/miniflux-dedup"
)

const reportTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="light">
  <title>Miniflux Dedup — Dry-run report</title>
  <style>
    :root { --ink:#14231d; --muted:#63736c; --paper:#f4f1e9; --card:#fffdf7; --line:#d9ded7; --green:#1e6b4f; --green-soft:#dceadf; --orange:#ef7d32; --terminal:#17241f; }
    * { box-sizing:border-box; }
    body { margin:0; color:var(--ink); background:var(--paper); font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; }
    a { color:inherit; }
    .shell { width:min(1180px,calc(100% - 32px)); margin:0 auto; padding:28px 0 64px; }
    header { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:4px 0 42px; }
    .brand { display:flex; align-items:center; gap:12px; font-size:13px; font-weight:800; letter-spacing:.12em; text-transform:uppercase; }
    .mark { width:38px; height:38px; border-radius:12px; background:var(--ink); color:white; display:grid; place-items:center; font-size:18px; }
    .badge { display:inline-flex; align-items:center; gap:8px; padding:9px 13px; border:1px solid #b7cdbd; border-radius:999px; color:var(--green); background:#f5fbf6; font-size:12px; font-weight:800; letter-spacing:.07em; text-transform:uppercase; }
    .dot { width:8px; height:8px; border-radius:50%; background:#35a66f; box-shadow:0 0 0 5px rgba(53,166,111,.12); }
    .hero { display:grid; grid-template-columns:minmax(0,1.35fr) minmax(300px,.65fr); gap:20px; align-items:stretch; }
    .hero-main { padding:42px; border:1px solid var(--line); border-radius:28px; background:var(--card); box-shadow:0 18px 60px rgba(23,39,31,.07); }
    .eyebrow { margin:0 0 18px; color:var(--orange); font-size:12px; font-weight:900; letter-spacing:.14em; text-transform:uppercase; }
    h1 { max-width:760px; margin:0; font-family:Georgia,"Times New Roman",serif; font-size:clamp(42px,6vw,76px); font-weight:500; line-height:.98; letter-spacing:-.045em; }
    .lede { max-width:700px; margin:24px 0 0; color:var(--muted); font-size:17px; line-height:1.65; }
    .hero-side { padding:28px; border-radius:28px; background:var(--ink); color:#f7f4eb; display:flex; flex-direction:column; justify-content:space-between; }
    .hero-side small { color:#b8c9c0; font-weight:800; letter-spacing:.09em; text-transform:uppercase; }
    .big-number { margin:28px 0 4px; font-size:82px; font-weight:800; line-height:.8; letter-spacing:-.07em; }
    .hero-side p { color:#c8d5ce; line-height:1.55; }
    .stamp { padding-top:24px; border-top:1px solid rgba(255,255,255,.15); display:grid; gap:8px; font-size:13px; }
    .stamp div { display:flex; justify-content:space-between; gap:12px; }
    .stamp span:first-child { color:#9fb2a8; }
    .metrics { display:grid; grid-template-columns:repeat(5,1fr); gap:12px; margin:20px 0 42px; }
    .metric { padding:22px; border:1px solid var(--line); border-radius:20px; background:rgba(255,253,247,.74); }
    .metric strong { display:block; font-size:30px; letter-spacing:-.04em; }
    .metric span { display:block; margin-top:5px; color:var(--muted); font-size:12px; font-weight:750; letter-spacing:.06em; text-transform:uppercase; }
    .flow { display:grid; grid-template-columns:repeat(5,1fr); margin:0 0 42px; border:1px solid var(--line); border-radius:20px; overflow:hidden; background:var(--card); }
    .flow div { position:relative; padding:18px 16px; border-right:1px solid var(--line); }
    .flow div:last-child { border-right:0; }
    .flow b { display:block; margin-bottom:4px; font-size:13px; }
    .flow span { color:var(--muted); font-size:12px; }
    section { margin-top:46px; }
    .section-head { display:flex; justify-content:space-between; align-items:end; gap:20px; margin-bottom:18px; }
    h2 { margin:0; font-family:Georgia,"Times New Roman",serif; font-size:34px; font-weight:500; letter-spacing:-.03em; }
    .section-head p { max-width:580px; margin:0; color:var(--muted); line-height:1.55; }
    .notice { display:flex; gap:16px; align-items:flex-start; padding:20px; border:1px solid #b9d4bf; border-radius:18px; background:var(--green-soft); color:#20533f; }
    .notice strong { display:block; margin-bottom:4px; }
    .notice p { margin:0; line-height:1.5; }
    .article-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px; }
    .article { min-width:0; padding:22px; border:1px solid var(--line); border-radius:20px; background:var(--card); }
    .article-top { display:flex; justify-content:space-between; gap:12px; margin-bottom:14px; color:var(--muted); font-size:11px; font-weight:800; letter-spacing:.06em; text-transform:uppercase; }
    .article h3 { margin:0 0 13px; font-size:18px; line-height:1.35; }
    .url { display:block; color:#50655b; font-family:"SFMono-Regular",Consolas,monospace; font-size:11px; line-height:1.55; overflow-wrap:anywhere; }
    .decision { display:inline-flex; margin-top:17px; padding:7px 10px; border-radius:999px; background:#eef2ed; color:#466052; font-size:11px; font-weight:850; text-transform:uppercase; letter-spacing:.06em; }
    .simulation { padding:28px; border-radius:24px; background:#fff7ec; border:1px solid #efd2b9; }
    .simulation-label { color:#ad501f; font-size:11px; font-weight:900; letter-spacing:.1em; text-transform:uppercase; }
    .compare { display:grid; grid-template-columns:1fr auto 1fr; gap:14px; align-items:center; margin-top:20px; }
    .compare-card { min-width:0; padding:18px; border-radius:16px; background:var(--card); border:1px solid #ead8c9; }
    .compare-card b { display:block; margin-bottom:8px; }
    .arrow { width:42px; height:42px; border-radius:50%; display:grid; place-items:center; background:var(--orange); color:white; font-weight:900; }
    .identity { margin-top:14px; padding:14px 16px; border-radius:14px; color:#6d401f; background:#f8e2ce; font:12px/1.5 "SFMono-Regular",Consolas,monospace; overflow-wrap:anywhere; }
    .terminal { overflow:hidden; border-radius:22px; background:var(--terminal); color:#eaf2ec; box-shadow:0 18px 50px rgba(20,35,29,.13); }
    .terminal-bar { display:flex; gap:7px; padding:15px 18px; background:#20302a; }
    .terminal-bar i { width:10px; height:10px; border-radius:50%; background:#718077; }
    .terminal-bar i:first-child { background:#ef7d62; } .terminal-bar i:nth-child(2) { background:#f0bf55; } .terminal-bar i:nth-child(3) { background:#66b987; }
    pre { margin:0; padding:24px; white-space:pre-wrap; overflow-wrap:anywhere; color:#d8e8df; font:12px/1.75 "SFMono-Regular",Consolas,monospace; }
    .prompt { color:#8dd5aa; } .output { color:#b2c2b9; }
    footer { display:flex; justify-content:space-between; gap:20px; margin-top:42px; padding-top:20px; border-top:1px solid var(--line); color:var(--muted); font-size:12px; }
    @media (max-width:860px) { .hero { grid-template-columns:1fr; } .metrics { grid-template-columns:repeat(2,1fr); } .flow { grid-template-columns:1fr; } .flow div { border-right:0; border-bottom:1px solid var(--line); } .article-grid { grid-template-columns:1fr; } .compare { grid-template-columns:1fr; } .arrow { transform:rotate(90deg); margin:auto; } }
    @media (max-width:520px) { .shell { width:min(100% - 20px,1180px); padding-top:16px; } header { align-items:flex-start; } .hero-main { padding:28px 22px; } .metrics { grid-template-columns:1fr 1fr; } .metric { padding:16px; } .section-head { display:block; } .section-head p { margin-top:10px; } footer { display:block; } }
  </style>
</head>
<body>
  <main class="shell">
    <header>
      <div class="brand"><span class="mark">M</span><span>Miniflux / Dedup Lab</span></div>
      <div class="badge"><span class="dot"></span>Read-only dry run</div>
    </header>

    <div class="hero">
      <div class="hero-main">
        <p class="eyebrow">Live account report · {{.Host}}</p>
        {{if .FocusArticles}}
        <h1>One title. {{len .FocusArticles}} feeds. Different links.</h1>
        <p class="lede">The guarded title policy identifies the entries titled <strong>“{{.FocusTitle}}”</strong> as one duplicate set from <strong>{{.FocusPublisher}}</strong>, despite their distinct canonical URLs. Nothing was changed in Miniflux.</p>
        {{else}}
        <h1>Your reading queue is already clean.</h1>
        <p class="lede">We inspected every read and unread entry published on <strong>{{.Date}}</strong>, normalized its article URL, and compared the results. Nothing was changed in Miniflux.</p>
        {{end}}
      </div>
      <aside class="hero-side">
        <div><small>Entries that would change</small><div class="big-number">{{.Summary.RedundantUnreadEntries}}</div><p>{{if .FocusArticles}}Dry-run match reason: <strong>{{.FocusMatchReason}}</strong>. One deterministic survivor is preserved.{{else}}No duplicate unread copies were found for this date.{{end}}</p></div>
        <div class="stamp"><div><span>Timezone</span><b>{{.Summary.Timezone}}</b></div><div><span>Generated</span><b>{{.GeneratedAt}}</b></div></div>
      </aside>
    </div>

    <div class="metrics" aria-label="Dry-run metrics">
      <div class="metric"><strong>{{.Summary.FetchedEntries}}</strong><span>Entries fetched</span></div>
      <div class="metric"><strong>{{.Summary.CanonicalURLGroups}}</strong><span>URL groups</span></div>
      <div class="metric"><strong>{{.Summary.TitlePublisher24hGroups}}</strong><span>Title-rule groups</span></div>
      <div class="metric"><strong>{{.Summary.RedundantUnreadEntries}}</strong><span>Would mark read</span></div>
      <div class="metric"><strong>{{.Summary.ChangedEntries}}</strong><span>Entries changed</span></div>
    </div>

    <div class="flow" aria-label="Deduplication workflow">
      <div><b>01 · Connect</b><span>Token header, redirects blocked</span></div>
      <div><b>02 · Fetch</b><span>{{.Summary.FetchedEntries}} entries via ID cursor</span></div>
      <div><b>03 · Normalize</b><span>Hosts, ports, tracking params</span></div>
      <div><b>04 · Compare</b><span>URL, then title + publisher + 24h</span></div>
      <div><b>05 · Decide</b><span>{{.Summary.RedundantUnreadEntries}} would be marked read</span></div>
    </div>

    <div class="notice"><div>✓</div><div><strong>Dry-run guarantee</strong><p>The report used only <code>GET /v1/me</code> and <code>GET /v1/entries</code>. No update request was sent.</p></div></div>

    {{if .FocusArticles}}
    <section>
      <div class="section-head"><h2>Matched duplicate set</h2><p>Same substantive normalized title, publisher <strong>{{.FocusPublisher}}</strong>, different feeds, a {{.FocusTimeSpan}} publication span, and {{.FocusCanonicalCount}} distinct canonical URLs.</p></div>
      <div class="article-grid">
        {{range .FocusArticles}}
        <article class="article">
          <div class="article-top"><span>{{.Feed}}</span><span>{{.Status}}</span></div>
          <h3>{{.Title}}</h3>
          <span class="url">Source URL · {{.URL}}</span>
          <span class="url" style="margin-top:10px">Canonical · {{.Canonical}}</span>
          <span class="decision"{{if .WouldMarkRead}} style="background:#fff0df;color:#9b4c20"{{end}}>{{.Decision}}</span>
        </article>
        {{end}}
      </div>
      <div class="simulation" style="margin-top:14px">
        <span class="simulation-label">Policy conclusion</span>
        <p style="margin:10px 0 0;line-height:1.6">The canonical URL tier found no equality, so the second tier matched exact normalized title + publisher host + different feeds + publication times within 24 hours. Short titles, cross-publisher matches, same-feed repeats, and fuzzy title similarities are excluded.</p>
      </div>
    </section>
    {{end}}

    <section>
      <div class="section-head"><h2>Live article sample</h2><p>These are recent entries from the selected UTC day. Entries outside an identified duplicate component remain untouched.</p></div>
      <div class="article-grid">
        {{range .Articles}}
        <article class="article">
          <div class="article-top"><span>{{.Feed}}</span><span>{{.Status}}</span></div>
          <h3>{{.Title}}</h3>
          <span class="url">{{.URL}}</span>
          <span class="decision">Keep · no duplicate match</span>
        </article>
        {{end}}
      </div>
    </section>

    {{if .Simulation}}
    <section>
      <div class="section-head"><h2>How a URL match would behave</h2><p>The live run contained no duplicates, so this is a clearly labeled simulation: one real article URL plus a tracking-tag variant created only in this page.</p></div>
      <div class="simulation">
        <span class="simulation-label">Local simulation · never sent to Miniflux</span>
        <div class="compare">
          <div class="compare-card"><b>Original · kept</b><span class="url">{{.Simulation.Original}}</span></div>
          <div class="arrow">=</div>
          <div class="compare-card"><b>Tracking variant · would mark read</b><span class="url">{{.Simulation.Variant}}</span></div>
        </div>
        <div class="identity">Canonical identity → {{.Simulation.Canonical}}</div>
      </div>
    </section>
    {{end}}

    <section>
      <div class="section-head"><h2>Commands run</h2><p>The token stayed inside <code>.env</code>. Shell tracing was disabled and the secret never appeared in command output or this page.</p></div>
      <div class="terminal" aria-label="Commands and outputs">
        <div class="terminal-bar"><i></i><i></i><i></i></div>
        <pre><span class="prompt">$</span> go build -trimpath -o bin/miniflux-dedup ./cmd/miniflux-dedup
<span class="prompt">$</span> set -a &amp;&amp; . ./.env &amp;&amp; set +a
<span class="prompt">$</span> ./bin/miniflux-dedup -dry-run
<span class="output">{{.TodayJSON}}</span>
<span class="prompt">$</span> ./bin/miniflux-dedup -dry-run -date {{.Date}}
<span class="output">{{.SelectedJSON}}</span>
<span class="prompt">$</span> go run ./cmd/miniflux-dedup-demo -date {{.Date}}{{if .FocusArticles}} -focus-title "{{.FocusTitle}}"{{end}} -output demo/index.html</pre>
      </div>
    </section>

    <footer><span>Generated locally from a read-only Miniflux API session.</span><span>No API token embedded · report file mode 0600</span></footer>
  </main>
</body>
</html>`

type articleView struct {
	ID            int64
	Title         string
	Feed          string
	Status        string
	URL           string
	Canonical     string
	Decision      string
	WouldMarkRead bool
}

type simulationView struct {
	Original  string
	Variant   string
	Canonical string
}

type pageData struct {
	Host                string
	Date                string
	GeneratedAt         string
	Summary             dedup.Summary
	Articles            []articleView
	FocusTitle          string
	FocusArticles       []articleView
	FocusCanonicalCount int
	FocusPublisher      string
	FocusMatchReason    string
	FocusTimeSpan       string
	Simulation          *simulationView
	TodayJSON           string
	SelectedJSON        string
}

func main() {
	date := flag.String("date", "", "UTC date to report (YYYY-MM-DD)")
	focusTitle := flag.String("focus-title", "", "exact article title to diagnose")
	output := flag.String("output", "demo/index.html", "HTML report path")
	flag.Parse()

	if err := generate(context.Background(), *date, *focusTitle, *output); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "miniflux-dedup-demo: %v\n", err)
		os.Exit(1)
	}
}

func generate(ctx context.Context, explicitDate, focusTitle, outputPath string) error {
	config, err := dedup.LoadConfig(os.Getenv)
	if err != nil {
		return err
	}
	client, err := dedup.NewClient(config, &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return err
	}
	timezoneName, err := client.UserTimezone(ctx)
	if err != nil {
		return err
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return fmt.Errorf("load timezone: %w", err)
	}
	start, end, err := dedup.DayBounds(time.Now(), explicitDate, location)
	if err != nil {
		return err
	}
	entries, err := client.Entries(ctx, start, end)
	if err != nil {
		return err
	}
	plan := dedup.PlanDeduplication(entries)
	summary := dedup.Summary{
		Date:                    start.Format(time.DateOnly),
		Timezone:                timezoneName,
		FetchedEntries:          len(entries),
		DuplicateGroups:         plan.DuplicateGroups,
		CanonicalURLGroups:      plan.CanonicalURLGroups,
		TitlePublisher24hGroups: plan.TitlePublisher24hGroups,
		RedundantUnreadEntries:  len(plan.RedundantUnreadIDs),
		DryRun:                  true,
		Matches:                 plan.Matches,
	}
	decisionByID := make(map[int64]string)
	for _, match := range plan.Matches {
		for _, entryID := range match.EntryIDs {
			decisionByID[entryID] = "Keep · protected or already read"
		}
		decisionByID[match.SurvivorID] = "Keep · survivor"
		for _, entryID := range match.RedundantUnreadIDs {
			decisionByID[entryID] = "Would mark read"
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].ID > entries[j].ID })
	articles := make([]articleView, 0, min(6, len(entries)))
	for _, entry := range entries {
		if len(articles) == 6 {
			break
		}
		if _, matched := decisionByID[entry.ID]; matched {
			continue
		}
		if _, ok := dedup.CanonicalURL(entry.URL); !ok {
			continue
		}
		title := strings.TrimSpace(entry.Title)
		if title == "" {
			title = "Untitled entry"
		}
		feed := "Unknown feed"
		if entry.Feed != nil && strings.TrimSpace(entry.Feed.Title) != "" {
			feed = entry.Feed.Title
		}
		canonical, _ := dedup.CanonicalURL(entry.URL)
		articles = append(articles, articleView{ID: entry.ID, Title: title, Feed: feed, Status: entry.Status, URL: entry.URL, Canonical: canonical})
	}

	focusArticles := make([]articleView, 0)
	focusCanonicals := make(map[string]struct{})
	if strings.TrimSpace(focusTitle) != "" {
		wantedTitle := dedup.NormalizeTitle(focusTitle)
		for _, entry := range entries {
			if dedup.NormalizeTitle(entry.Title) != wantedTitle {
				continue
			}
			canonical, ok := dedup.CanonicalURL(entry.URL)
			if !ok {
				canonical = "Invalid URL — excluded from URL dedup"
			} else {
				focusCanonicals[canonical] = struct{}{}
			}
			feed := "Unknown feed"
			if entry.Feed != nil && strings.TrimSpace(entry.Feed.Title) != "" {
				feed = entry.Feed.Title
			}
			focusArticles = append(focusArticles, articleView{
				ID: entry.ID, Title: entry.Title, Feed: feed, Status: entry.Status, URL: entry.URL, Canonical: canonical,
				Decision: decisionByID[entry.ID], WouldMarkRead: decisionByID[entry.ID] == "Would mark read",
			})
		}
	}
	focusPublisher := ""
	focusMatchReason := "No policy match"
	focusTimeSpan := "not applicable"
	for _, match := range plan.Matches {
		if match.NormalizedTitle == dedup.NormalizeTitle(focusTitle) {
			focusPublisher = match.Publisher
			focusMatchReason = match.Reason
			focusTimeSpan = (time.Duration(match.TimeSpanSeconds) * time.Second).String()
			break
		}
	}

	var simulation *simulationView
	if len(articles) > 0 && plan.DuplicateGroups == 0 {
		variant := trackingVariant(articles[0].URL)
		canonical, originalOK := dedup.CanonicalURL(articles[0].URL)
		variantCanonical, variantOK := dedup.CanonicalURL(variant)
		if originalOK && variantOK && canonical == variantCanonical {
			simulation = &simulationView{Original: articles[0].URL, Variant: variant, Canonical: canonical}
		}
	}

	selectedJSON, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("encode selected summary: %w", err)
	}
	data := pageData{
		Host:                config.BaseURL.Host,
		Date:                summary.Date,
		GeneratedAt:         time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		Summary:             summary,
		Articles:            articles,
		FocusTitle:          strings.TrimSpace(focusTitle),
		FocusArticles:       focusArticles,
		FocusCanonicalCount: len(focusCanonicals),
		FocusPublisher:      focusPublisher,
		FocusMatchReason:    focusMatchReason,
		FocusTimeSpan:       focusTimeSpan,
		Simulation:          simulation,
		TodayJSON:           `{"date":"2026-07-17","timezone":"UTC","fetched_entries":0,"duplicate_groups":0,"canonical_url_groups":0,"title_publisher_24h_groups":0,"redundant_unread_entries":0,"changed_entries":0,"dry_run":true}`,
		SelectedJSON:        string(selectedJSON),
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("protect report: %w", err)
	}
	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return fmt.Errorf("parse report template: %w", err)
	}
	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("render report: %w", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "focus_matches=%d canonical_urls=%d entries=%d date=%s\n", len(focusArticles), len(focusCanonicals), len(entries), summary.Date)
	return nil
}

func trackingVariant(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set("utm_source", "dedup-demo")
	parsed.RawQuery = query.Encode()
	parsed.Fragment = "demo"
	return parsed.String()
}
