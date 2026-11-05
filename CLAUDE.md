# go-slow

Slow news, acting like the Economist: a Go web app, hypermedia-driven with htmx, storing the Record in SQLite.

@~/.claude/commons/guides/global.md

@~/.claude/commons/guides/agentic-development.md

@~/.claude/commons/guides/project-knowledge.md

@~/.claude/commons/guides/web-apps.md

@~/.claude/commons/guides/model-calls.md

**Deviation from the Global Guide's *Checks and CI*:** the Guide has CI run `pre-commit run --all-files` and treats every hook alike. Here CI runs the hooks on a PR's changed files (`--from-ref origin/<base> --to-ref HEAD`; `--all-files` only when a PR changes `.pre-commit-config.yaml` or a workflow, and on a push to `main`), and the `go-test` hook is at the manual stage, so it never runs on a local commit or push and only CI runs it. The reason is the Go tests' cost: `go test -race ./...` takes minutes (about 4 on CI's runner), against seconds for every other hook; `--all-files` would charge it to every PR, docs-only ones included, and running it on a local commit and in the check before a PR would pay it again on top of CI. The cost of the change: a failure only the full suite catches costs one CI round trip, and each hook's `files:` filter must name everything the hook depends on, or CI skips a check it needed.

There is no Go Guide yet; the Go tooling below is this repo's own until there is one. The Model calls Guide covers the Editor's acts; the system design's *The Editor* section is how go-slow applies it.

## Where things are

This is a map. A fact about the code lives in the code: each package's doc comment (`go doc ./internal/<pkg>`) is its overview, and its identifiers' comments the detail. A reason lives in the system design.

The docs state the design, and the code still builds an earlier one until epic #294's slices land. So the code, the commands and the config below still name its acts (reading, relating, the Briefing, the Sunday read, Feedback), which `CONTEXT.md` and the system design no longer use.

- [`CONTEXT.md`](CONTEXT.md): the domain language (Owner, Story, Span, Item...). Use its terms in new code, tests and issues.
- [`docs/system-design/README.md`](docs/system-design/README.md): the design, its end state and hypotheses, and why it has its shape.
- [`docs/research/`](docs/research/): research that grounds the design.
- [`internal/editor/testdata/recorded/README.md`](internal/editor/testdata/recorded/README.md): the Editor's recorded runs: what is where, what each act's recording asserts, refreshing frozen copies, the yield report.
- Code:
  - `cmd/go-slow`: `main`, the composition root (it alone reads the environment), and the admin commands (`admin.go`, `rejudge.go`).
  - `internal/config`: the typed config.
  - `internal/web`: the web adapter, the app's constructor `web.New`, its templates, the vendored htmx, its one stylesheet `static/go-slow.css` and its pages' shared layout, body and margin (`doc.go`).
  - `internal/collect`: Collection: the Sources list, the feed formats, fetching full text, `Backfill`, and the timetable `main` starts.
  - `internal/cut`: the Cut, the first event of the Record: `Sentences`, the sentence cutter, and `Cutter.CutNew`.
  - `internal/interest`: the Owner's Topics, `Topic.Touches` and `SourceCounts`; Topics select nothing.
  - `internal/feedback`: the Owner's Feedback, kept as events; `Pieces.Give`.
  - `internal/register`: the register, each hypothesis read from the Record by its declared rule, shown at `/register`.
  - `internal/claude`: the Claude model adapter (Messages, Message Batches and a conversation turn), with its prices (`prices.go`).
  - `internal/editor`: the Editor and its acts (reading, look-ups, relating, the Briefing, the Sunday read), its ports, batches, kept calls, precedents and re-judging (`doc.go`); `internal/editor/yield` is a dev tool.
  - `internal/wikidata`: the Wikidata adapter, polite by Wikimedia's rules; `wikidatatest` fakes it at HTTP from recordings.
  - `internal/polite`: the HTTP adapter Collection fetches through, carrying the politeness rules.
  - `internal/sqlite`: the storage adapter: the event store that is the Record, its views and its migrations (`doc.go`); `sqlitetest` gives tests a migrated Record.
  - `internal/server`: serving with graceful shutdown.

## Commands

- **Inner loop:** `go test ./<package>` on the packages being changed (add `-run <Test>` to narrow it). The full `go test ./...` is CI's (*Checks and CI*, below).
- **Before committing or opening a PR:** `uvx --from pre-commit==4.7.0 pre-commit run --all-files` runs the commit-stage hooks (every hook but `go-test`), plus `go test` on the packages changed. CI is the full run. A `pre-commit` already on PATH works too, if it is at least 4.5.0. Install the git hook once per clone with `uvx --from pre-commit==4.7.0 pre-commit install`: it installs the pre-commit stage only, so a commit runs the fast hooks and never `go test`.
- **Full check, as CI runs it:** `uvx --from pre-commit==4.7.0 pre-commit run --hook-stage manual --from-ref origin/main --to-ref HEAD` (every hook, `go-test` included, on the files changed since `main`; `--all-files` for every file). It takes about 12 minutes when `go-test` runs; run it only when asked to, and in the background.
- **Lint and format:** `go tool -modfile=tools/go.mod golangci-lint run ./...` and `... golangci-lint fmt`.
- **Run locally:** `cp .env.example .env`, then `docker compose up --build`, and open http://localhost:8080 (the port is published on `127.0.0.1` only; the Record is the host folder `RECORD_DIR`, bind-mounted). For daily use, see the *Runbook* below. The app logs JSON lines to stdout and stops cleanly on SIGTERM (`docker compose stop`). Outside docker: `go run ./cmd/go-slow` with the variables in `.env.example` set.

### Re-recording the Editor's acts

Recording calls the real model and costs money; the suite only replays. Re-record deliberately, when an act's instructions, schema, model or precedents change, and only that act. Each command needs `GOSLOW_ANTHROPIC_API_KEY` and the act's model variable, and `-timeout 90m` (the default 10m is too short for five samples of every fixture); commit the recordings. How: the Model calls Guide's detail files (`recording.md`, `samples.md`, `changing-an-act.md`); why: system design, *The Editor* (*A recording is one sample…*, *Each act is recorded over its own frozen inputs*); go-slow's paths, properties and checks: [`internal/editor/testdata/recorded/README.md`](internal/editor/testdata/recorded/README.md).

**The Owner's standing say (2026-11-03), while the design is being redrawn (#294):** an agent re-records or refreshes any act without asking, the Sunday read included, up to $10 a recording run, and says what it spent. Speed of iteration matters more than the cost of a recording. Where a line below says "only on the Owner's say", this is that say, up to the cap.

- **Reading:** `go test -count=1 -timeout 90m ./internal/editor -run TestRecordedReadings -record`, with `READING_MODEL`. Five calls per fixture, more where the checks refuse an attempt.
- **Relating:** `go test -count=1 -timeout 90m ./internal/editor -run TestRecordedRelatings -record`, with `RELATING_MODEL`.
- **The Briefing:** `go test -count=1 -timeout 90m ./internal/editor -run '^TestRecordedBriefing$' -record`, with `BRIEFING_MODEL` (on Claude Haiku 5.5, about a cent a sample: some 30k tokens in, 6–27k out). Iterate on its instructions on single samples first, and look at them, before recording.
- **The precedent cases:** `go test -count=1 -timeout 90m ./internal/editor -run '^TestRecordedPrecedentRelating$' -record` with `RELATING_MODEL`, and `-run '^TestRecordedPrecedentBriefing$'` with `BRIEFING_MODEL` (on Claude Haiku 5.5, under a cent a sample for relating, about one for the Briefing).
- **The Sunday read, only on the Owner's say** (it runs the agent on Claude Opus 5.5 three times and costs about a dollar, some $0.3 a sample in 2026-10's run): `go test -count=1 -timeout 90m ./internal/editor -run '^TestRecordedSundayRead$' -record`, with `SUNDAY_READ_MODEL`. `-max-tries N` bounds each sample's tries below the act's own `SundayReadTries`, to keep a run within the retries its brief allows; `-max-samples 1` draws one sample alone, to probe a changed act before paying for three.
- **Refresh an act's frozen copies** (to bring a downstream act onto an upstream act's newer recordings): its own change, never part of the upstream act's; every upstream act's recordings copied together (the Sunday read's in their own commit); in chain order (relating, the Briefing, the Sunday read); the Sunday read only on the Owner's say. Steps: the README's *Refreshing*.
- **Report each act's yield on every re-recording, and paste the table into the PR body**: extract the recordings being replaced (`git archive -o <scratch>/old.tar origin/main internal/editor/testdata/recorded`, then `tar -xf <scratch>/old.tar -C <scratch>/old`), then `go run ./internal/editor/yield <scratch>/old/internal/editor/testdata/recorded internal/editor/testdata/recorded`. Calls no model; what it prints is in the README.

### Admin commands

Each is the app's binary with its own config (`docker compose run --rm app <command>`, or outside docker `go run ./cmd/go-slow <command>` with the same environment); what each does is its doc comment in `cmd/go-slow`. Run one against the Owner's Record only with the Owner's say (*Runbook*, below).

- **Record commits for the register** (the quiet window, and Due on a change to the Editor): `git log --format='commit %H %cI' --name-only | docker compose run --rm -T app commits`. The app can't see the repository; ones already kept are skipped, so pipe the whole log each time.
- **Retry failed work** (work that failed all its attempts, listed on the Items page): `docker compose run --rm app retry [reading|relating]`. It clears the failed marks on that act's work (every act's with none named), so the next Collection run submits it again.
- **Backfill Items' text** (one-off, for Items collected before their Source's text was fetched): `docker compose run --rm app backfill`. The next Collection run cuts what it fetched; an Item whose fetch fails is logged and left for another run.
- **Re-judge a range of the Record** (only on the Owner's say; it calls the real model and costs money; never run against the Owner's Record by an agent without that say): `docker compose run --rm app rejudge reading --from YYYY-MM-DD --to YYYY-MM-DD` is the dry run (prints the estimate in dollars; calls no model). `--confirm --max-usd N` runs it, as the live judge on `READING_MODEL`, capped at `N` dollars; it is the one way Items collected before the switch are read. `--candidate` (`--model M`) keeps a candidate instead; then `rejudge candidates`, `rejudge compare <id>`, and `rejudge choose <id>` once `READING_MODEL` names its model.

## Runbook: go-slow on the laptop

go-slow runs on the Owner's laptop, from this repo's image and `docker-compose.yml`, published on `127.0.0.1` only (system design: *go-slow runs when the Owner starts it*). Nothing keeps it running: start it each day to read.

- **Docker without Docker Desktop:** the containers run in Colima, which needs no login. Once: `brew install colima docker docker-compose`, add `"cliPluginsExtraDirs": ["/opt/homebrew/lib/docker/cli-plugins"]` to `~/.docker/config.json` so `docker compose` finds the plugin, then `colima start --memory 4 --cpu 4` (it switches the docker context to `colima`, and later starts keep the size). The VM needs the 4 GiB: compiling `anthropic-sdk-go`, one 120k-line generated package, peaks at about 1.4 GB in one compiler process, and Colima's default 2 GiB VM kills the image build.
- **Once:** `cp .env.example .env`, set your own `COLLECTION_CONTACT` and `GOSLOW_ANTHROPIC_API_KEY`, and set `RECORD_DIR` to an absolute path outside the repo (the folder holding the Record; `mkdir -p` it). Copying that folder while go-slow is stopped is a backup.
- **Start each day:** `colima start` (if it isn't running; `colima status` says), `docker compose up -d`, then open http://localhost:8080. The start's Collection run fetches what came in overnight and submits it in batches; at `BRIEFING_TIME` the Briefing is written over what the Record holds then, without waiting for the batches.
- **Stop:** `docker compose stop` (SIGTERM; the app drains and exits). `docker compose logs app` shows its JSON log.
- **Upgrade:** `git switch main`, `git pull`, then `docker compose up --build -d`. Migrations run on start. The start that applies `0032_events_only.sql` drops the old design's judgements and keeps the switch: the Items already in the Record are read only by `rejudge` (*Commands*), on the Owner's say.
- **Record commits for the register:** see *Admin commands*. Pipe the whole log each time.

**An agent starts the day's run when it notices it hasn't been done** (the Owner's standing say, 2026-10-27): if `docker compose ps` shows no app running on a day of work, the orchestrating agent runs *Start each day* (rebuilding with `--build` when `main` has moved since the image was built) and tells the Owner. That is the one way an agent touches the Record: never a sub-agent, never `docker compose run` of an admin command, a migration by hand or a copy, move or edit of `RECORD_DIR`, without the Owner's say for that act. The Record is the Owner's.

`COLLECTION_CONTACT` is still the example placeholder, on the Owner's call while go-slow's volume is low (2026-10-27); a real contact goes in before it fetches much more.

## Config

Only from environment variables, every one listed with a safe local value in `.env.example`, and all required: a missing one fails at start. The real `.env` is gitignored. Add a variable to `internal/config` and `.env.example` together. The model each act runs on is a variable (`READING_MODEL`, `RELATING_MODEL`, `BRIEFING_MODEL`, `SUNDAY_READ_MODEL`, each a Claude model, since Claude is the one adapter), always a pinned model ID, never an alias the provider can move (`claude-haiku-5-5` and `claude-opus-5-5` are fixed snapshots; how that was checked is in [`docs/research/models.md`](docs/research/models.md)). **A model change is a re-record, like an instructions change**: the model is part of the act's request, so re-record that act's recordings on the new model (*Commands*, above) in the same change. Each result keeps the model the provider reports, not the one configured; `BRIEFING_TIME` is when the Briefing is written and due by, `HH:MM` in `TZ`; `GOSLOW_ANTHROPIC_API_KEY` is a `config.Secret`, which never formats as its value. Never print, log or commit a real key.

## Go toolchain

- **Go is pinned by the `go` directive in `go.mod`** (1.27.3), the same in `tools/go.mod`. A local Go that is older downloads the pinned toolchain itself (`GOTOOLCHAIN=auto`, the default), CI's `actions/setup-go` reads `go.mod`, and the `Dockerfile`'s `golang:` tag repeats the version. Upgrade all four in one PR.
- **Tools are pinned in their own module**, `tools/go.mod` (`tool` directive), and run with `go tool -modfile=tools/go.mod <tool>`. A separate module keeps golangci-lint's hundreds of dependencies out of the app's `go.mod` and build. golangci-lint is at v2.14.0; its config is `.golangci.yml` (the `standard` linters plus a few for HTTP, SQL and errors; gofmt and goimports as formatters).
- **SQLite is `modernc.org/sqlite`**, pure Go, so the binary builds with `CGO_ENABLED=0` and runs on a distroless static image. Migrations are embedded SQL files in `internal/sqlite/migrations`, applied in name order by `sqlite.Open`, with the count applied kept in `PRAGMA user_version`. Never edit a migration that has been merged; add the next one. Migrations run on one connection with foreign keys off, each checked with `PRAGMA foreign_key_check` before it commits, so one can rebuild a table others refer to by SQLite's procedure (Web apps Guide, *Storage*: create `new_X`, copy, drop `X`, rename `new_X` to `X`). `0014_sunday_read.sql` rebuilt `feedback` in the other order (renaming the old table first, under `PRAGMA legacy_alter_table = ON`); it is merged, so it stays, but the next rebuild follows SQLite's order.
- **htmx is vendored** at `internal/web/static/htmx-2.0.10.min.js`, byte-identical to the npm release (sha256 `71ea6718…c0de`), and excluded from the whitespace hooks so it stays so. Upgrading is replacing the file and the `<script>` in `templates/layout.html`.

## Checks and CI

- pre-commit hooks: the Global Guide's (`pre-commit-hooks`, actionlint, shellcheck; actionlint comes from `actionlint-py`, with `shellcheck-py` in its `additional_dependencies` for the shellcheck on its PATH, as the Rust Guide does), then `go mod tidy -diff` for both modules, `golangci-lint fmt`, `golangci-lint run` and `go test -race ./...` (`go-test`, at the manual stage only). The Go hooks are `language: system`, so they need `go` on PATH. Each hook's `files:` filter names everything it depends on (for `go-test`, every `testdata/`, `.env.example` and the checks' own config), since in CI the filter decides whether it runs.
- CI is `.github/workflows/ci.yml`: one job, **`pre-commit`**, on every PR, and on pushes to `main` that change `go.mod`, `go.sum`, `tools/` pins or `.pre-commit-config.yaml`. It runs every hook at the manual stage, in one run: on a PR over the files it changes, from the merge base with its base branch (every file when it changes `.pre-commit-config.yaml` or a workflow), and on a push over every file (the Deviation above). **The required status check is `pre-commit`.** Renaming the job breaks merging once the `main` ruleset exists (it is created after CI first reports under this name).
- Actions and images are pinned to exact versions (the distroless base by digest).

## Known traps

- **Never name go-slow's key `ANTHROPIC_API_KEY`.** Claude Code reads that variable too: set in the shell `claude` is launched from, it switches Claude Code from the claude.ai login to the key, for its own requests and billing, and claude.ai connectors stop working. go-slow reads `GOSLOW_ANTHROPIC_API_KEY` instead, which agents inherit without Claude Code taking it. *Remove when:* never, while go-slow is built with Claude Code.
