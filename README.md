# miniflux-dedup

`miniflux-dedup` is a small program that removes cross-feed duplicates from the Miniflux unread workflow. It fetches entries published today in the Miniflux user's timezone, keeps one deterministic copy of each matched article, and marks safe redundant unread copies as read.

It does not delete entries. Current Miniflux versions expose `read` and `unread` through this API, not an entry-level hard-delete operation.

The program has no background loop, daemon, cron integration, or built-in scheduler. Each invocation processes one day and exits. Run it manually when needed, or configure and maintain an external scheduler such as system cron, a systemd timer, Kubernetes CronJob, or another automation service.

## How it works

Each time the program runs, it:

1. Calls `GET /v1/me` to read the user's IANA timezone.
2. Calculates `[local midnight, next local midnight)` for today, or for `-date` when supplied.
3. Fetches every read and unread entry in that interval from `GET /v1/entries`, following ID-cursor pagination before changing anything and filtering the returned timestamps back to the exact interval.
4. Groups entries by canonical HTTP(S) URL, then joins distinct URLs only when they have the same guarded title identity described below.
5. Chooses one survivor per group, preferring starred, then unread, then the lowest Miniflux entry ID. Starred entries are never automatically marked read, including additional starred copies in the same group.
6. Marks only redundant unread IDs as `read` with `PUT /v1/entries`.

"Today" refers to the feed item's `published_at`, not when Miniflux discovered it. The program requires Miniflux 2.0.49 or newer because it uses the `published_after` and `published_before` filters. See the [official API reference](https://miniflux.app/docs/api.html).

URL matching is the strongest tier. It lowercases the scheme and host, removes fragments and default ports, makes an empty path `/`, sorts query parameters, and removes `utm_*`, `_hsenc`, `_hsmi`, `dclid`, `fbclid`, `gclid`, `igshid`, `mc_cid`, `mc_eid`, `mkt_tok`, `msclkid`, and `vero_id`.

When canonical URLs remain different, the second tier matches entries only when all of these safeguards hold:

- The titles are exactly equal after decoding HTML entities, lowercasing Unicode letters, treating punctuation as separators, and collapsing whitespace.
- The normalized title has at least four tokens and 20 Unicode characters.
- The article URLs have the same hostname, with only `www.` and the apex hostname treated as equivalent. Other subdomains remain distinct.
- The entries come from different Miniflux feed IDs.
- Their publication timestamps fall within one 24-hour window.

The title matcher is exact, not fuzzy. Invalid, relative, and non-HTTP(S) URLs, missing feed metadata, short titles, same-feed repeats, cross-host entries, and near-matching titles are never joined by this tier.

## Build and test

Go 1.25.12 or newer is required. This minimum includes the current standard-library security fixes; the runtime has no third-party dependencies.

```sh
make check
```

That command checks formatting, verifies and runs `golangci-lint` 2.7.2, runs `go vet`, executes all tests with the race detector, performs an executable-level fake-server validation, and builds `bin/miniflux-dedup`.

To build only:

```sh
make build
```

## Configure

Create an API key under Miniflux **Settings → API Keys**, then export:

```sh
export MINIFLUX_URL='https://reader.example.com'
export MINIFLUX_API_KEY='replace-with-your-api-key'
```

| Variable | Required | Description |
| --- | --- | --- |
| `MINIFLUX_URL` | Yes | Absolute HTTP(S) URL of the Miniflux server. It may include a base path, but not credentials, a query, or a fragment. |
| `MINIFLUX_API_KEY` | Yes | Miniflux API key created under **Settings → API Keys**. It is sent only in the `X-Auth-Token` request header. |
| `MINIFLUX_API_TOKEN` | Compatibility alias | Accepted when `MINIFLUX_API_KEY` is empty so existing installations continue to work. If both are set, `MINIFLUX_API_KEY` takes precedence. |

Use HTTPS unless Miniflux is reached over a trusted local network. Configure the final API URL: redirects are rejected so the authentication header cannot be forwarded to another origin. Credential values are redacted if a server includes one in an error response.

Available flags:

```text
-dry-run          find and report duplicates without changing entries
-date DATE        process DATE (YYYY-MM-DD) in the Miniflux user's timezone
-timeout DURATION per-request HTTP timeout (default 30s)
```

Validate credentials and matching before enabling mutation:

```sh
./bin/miniflux-dedup -dry-run
```

A successful run prints one JSON line suitable for terminal or automation logs:

```json
{"date":"2026-07-16","timezone":"America/Sao_Paulo","fetched_entries":42,"duplicate_groups":3,"canonical_url_groups":2,"title_publisher_24h_groups":1,"redundant_unread_entries":3,"changed_entries":3,"dry_run":false}
```

No matching duplicates is a successful no-op. A dry run also includes a `matches` audit array containing each group's match reason, entry IDs, URLs, survivor, safe redundant unread IDs, and—when the title tier matched—the publisher host, normalized title, and publication span. Normal mutation runs omit those verbose article details from output. Any configuration, HTTP, JSON, timezone, pagination, or update failure produces a non-zero exit status. To bound memory and runaway pagination, an invocation fails before mutation if an API response exceeds 64 MiB or the API reports more than 100,000 entries for the selected day.

## Run with Docker

Build the minimal, non-root image:

```sh
docker build -t miniflux-dedup:local .
```

Create a permission-restricted environment file from the example:

```sh
cp .env.example .env
chmod 0600 .env
```

The image defaults to dry-run mode. This reads Miniflux and reports matches without changing entries:

```sh
docker run --rm --env-file .env miniflux-dedup:local
```

Arguments passed after the image name replace the safe `-dry-run` default. Use an explicit dry run for a selected date:

```sh
docker run --rm --env-file .env miniflux-dedup:local \
  -dry-run -date 2026-07-14
```

Only after inspecting a dry run, omit `-dry-run` to allow changes. For today's entries, pass another explicit flag so the image's default command is replaced:

```sh
docker run --rm --env-file .env miniflux-dedup:local -timeout 30s
```

The container runs once and exits; it does not contain cron or an internal scheduler. You must invoke `docker run --rm` manually or schedule it externally. The final image contains only the static executable, CA certificates, and timezone data. It has no shell and runs as numeric non-root user `65532`.

## Optional: schedule externally with cron

The program does not install or manage cron. The repository only includes an optional `/etc/cron.d` configuration example. If you want hourly execution, you must manually install and maintain that file—or translate the command to your preferred external scheduler. Repeated invocations are idempotent because already-read redundant copies need no update.

```sh
sudo install -m 0755 bin/miniflux-dedup /usr/local/bin/miniflux-dedup
sudo install -m 0600 deploy/miniflux-dedup.env.example /etc/miniflux-dedup.env
sudo install -m 0644 deploy/miniflux-dedup.cron /etc/cron.d/miniflux-dedup
sudoedit /etc/miniflux-dedup.env
```

These commands merely copy the binary and example configuration; they do not start or enable a scheduler. The `/etc/cron.d` example runs as `root` and appends output to `/var/log/miniflux-dedup.log`. Change the user, paths, schedule, and logging destination to fit the host. Keep the environment file readable only by the scheduled operating-system user because it contains the API key.

Run the command manually with `-dry-run` before creating or enabling any external schedule:

```sh
. /etc/miniflux-dedup.env
/usr/local/bin/miniflux-dedup -dry-run
```

## Recovery and limitations

To stop future automated invocations, disable whichever external schedule you configured. Entries changed by the program remain in Miniflux and can be marked unread again from the UI.

The title tier deliberately accepts false negatives rather than using fuzzy similarity or merging across publisher hosts, subdomains, or calendar-day fetches. A duplicate pair split across two selected calendar days is not compared, even if its timestamps are less than 24 hours apart. Entries arriving or changing during pagination may be deferred until the next invocation. If a later update batch fails after earlier batches succeeded, the error reports the number already changed; the next idempotent invocation will converge the remainder.

## Generate a local HTML dry-run report

The demo report reads entries but never calls the update endpoint. It writes a mode-0600 self-contained HTML file so live article information stays local:

```sh
set -a && . ./.env && set +a
go run ./cmd/miniflux-dedup-demo \
  -date 2026-07-14 \
  -focus-title 'AWS Glue ETL Design Principles for Production PySpark Pipelines' \
  -output demo/index.html
```
