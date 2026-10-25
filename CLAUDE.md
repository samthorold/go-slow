# go-slow

A slow-news briefing: a Go web app, hypermedia-driven with htmx, storing the Record in SQLite.

@~/.claude/commons/guides/global.md

@~/.claude/commons/guides/agentic-development.md

@~/.claude/commons/guides/project-knowledge.md

@~/.claude/commons/guides/web-apps.md

There is no Go Guide yet; the Go tooling below is this repo's own until there is one.

## Where things are

- [`CONTEXT.md`](CONTEXT.md): the domain language (Briefing, Owner, Story, Item...). Use its terms in code, tests and issues.
- [`docs/system-design/README.md`](docs/system-design/README.md): the design, its end state and hypotheses, and why it has its shape.
- [`docs/research/`](docs/research/): research that grounds the design.
- Code:
  - `cmd/go-slow`: `main`, the composition root. It alone reads the environment.
  - `internal/config`: the typed config.
  - `internal/web`: the web adapter and the app's constructor, `web.New`, with its templates and the vendored htmx.
  - `internal/collect`: Collection, the Sources list with each one's terms (licence, attribution, what may be kept) and rate limit, reading each feed format (Atom, the ONS release search, Parliament's written statements) and the *No Source goes quiet* check, with its ports (`Fetcher`, `Store`). Its timetable runs inside the app process, started by `main`.
  - `internal/interest`: the Owner's Topics and Depths, and the lazy-reading check that ties Items to Topics with no model call. Collection runs the check after every run; taking up a Topic, or changing its Depth, re-runs it for that Topic over the Items not yet read.
  - `internal/editor`: the Editor. Its acts (*reading an Item* into Facts, Claims and Entities; *placing a Fact*: an Item's Facts weighed against the Stories that share their Entities, becoming Developments or starting new Stories; and *connecting*: once placed, an Item's Facts weighed against the Facts near them, giving each Entity its role in a Fact and making cause Connections between Facts), each with its own instructions, output schema and checks, and the provider-neutral ports they call: `Model` (one call, at once) and `Batcher` (a provider's batch interface). The daily acts run in batches through `Batches`: an act is a `BatchAct` (what awaits it, each piece of work with its request and how its result is checked and written), so placing (`Editor.Placing()`) and connecting (`Editor.Connecting()`) plug in beside `Editor.Reading()`; one batch carries every act's requests, and the Claude adapter sends each to its act's model (`claude.Options.ActModels`). Placing holds back an Item that shares an Entity with an earlier Item awaiting placing until that one is placed, so batches thread Stories as placing one Item at a time would. After each Collection run, `Batches` applies the results of batches that have ended (matched by request ID), resubmits each failed request alone, and submits what awaits as one batch; pending batches are kept in the Record. A reading that fails its checks writes nothing and is tried again. `Editor.ReadWaiting` is the synchronous path, for when no batch interface is available (likewise `PlaceWaiting` and `ConnectWaiting`). `Editor.Reconnect(ctx, now)` re-runs connecting in real time over the Facts placed in the day before `now`; `main` passes it to the Briefing's `Schedule` as a `Before` step. Connections between Stories are derived by the storage adapter (`ConnectedStories`), never stored.
  - `internal/claude`: the Claude model adapter (Anthropic's Messages API and Message Batches API through `anthropic-sdk-go`, structured output against the act's schema). Anthropic's types stay inside it.
  - `internal/editor` also writes the Briefing, in real time (`briefing.go`, `schedule.go`): `Schedule` is its own clock in the app process, writing the day's Briefing at `BRIEFING_TIME` through `Model.Complete`, not the batch runner. Its `Before` steps run just before writing: the hook for connecting over the day's Facts. Each statement cites its Developments by ref, and the act's check refuses a Briefing with an untraced statement; a failure is kept, shown on the day's page, and retried after `RetryBriefing`.
  - `internal/editor` also resolves a reading's Entities against Wikidata (`entities.go`, the `Wikidata` port) before it is written: matched Entities are Wikidata items, the rest the Editor's own, and the positions a matched person holds become Facts traced to the lookup.
  - `internal/wikidata`: the Wikidata adapter (Action API: `wbsearchentities`, `wbgetentities`), polite by Wikimedia's rules (go-slow's User-Agent, one request at a time a second apart, `maxlag`, `Retry-After` backoff). Tests fake it at HTTP with `wikidatatest.Handler`, serving real responses recorded once in `internal/wikidata/testdata/` (named by request; see the handler). Recording another is one `curl` with go-slow's User-Agent and a placeholder contact, never the Owner's.
  - `internal/polite`: the HTTP adapter Collection fetches through, carrying the politeness rules (User-Agent, conditional requests, one request per host at a time, `Retry-After` backoff, daily robots.txt).
  - `internal/sqlite`: the storage adapter, with its migrations.
  - `internal/server`: serving with graceful shutdown.

## Commands

- **Inner loop:** `go test ./...` (add `-run <Test>` or a package path to narrow it).
- **Full check:** `uvx --from pre-commit==4.7.0 pre-commit run --all-files`. It runs every hook below; CI runs exactly this. A `pre-commit` already on PATH works too, if it is at least 4.5.0. Install the git hook once per clone with `uvx --from pre-commit==4.7.0 pre-commit install`.
- **Re-record the Editor's recorded runs** (deliberately, when an act's instructions or schema change; it calls the real model once per fixture and costs money): `go test -count=1 ./internal/editor -run TestRecordedReadings -record`, with `GOSLOW_ANTHROPIC_API_KEY` and `READING_MODEL` set in the environment; for placing, `-run TestRecordedPlacings -record` with `PLACING_MODEL` set; for connecting, `-run TestRecordedConnectings -record` with `CONNECTING_MODEL` set. Placing's recordings build on the recorded readings, placed in publication order, and connecting's on both, so re-record placing after re-recording any reading, and connecting after either. Target the package: other packages don't define `-record`. The responses land in `internal/editor/testdata/recorded/`; commit them, and the diff shows what changed. The suite only replays them and never calls a model. A fixture with no recording is skipped with a message naming this command. Fixtures (`internal/editor/testdata/govuk/`) come only from admissible Sources, with their licence attribution.
- **Re-record the Briefing** (after placing is re-recorded, or the act's instructions or schema change): `go test -count=1 ./internal/editor -run TestRecordedBriefing -record`, with `GOSLOW_ANTHROPIC_API_KEY` and `BRIEFING_MODEL` set. It replays the recorded readings and placings into a Record with fixed Topics and Depths, and writes one Briefing (`testdata/recorded/briefing/`).
- **Lint and format:** `go tool -modfile=tools/go.mod golangci-lint run ./...` and `... golangci-lint fmt`.
- **Run locally:** `cp .env.example .env`, then `docker compose up --build`, and open http://localhost:8080. The app logs JSON lines to stdout and stops cleanly on SIGTERM (`docker compose stop`). Outside docker: `go run ./cmd/go-slow` with the variables in `.env.example` set.

## Config

Only from environment variables, every one listed with a safe local value in `.env.example`, and all required: a missing one fails at start. The real `.env` is gitignored. Add a variable to `internal/config` and `.env.example` together. The model each act runs on is a variable (`READING_MODEL`, `PLACING_MODEL`, `BRIEFING_MODEL`, each a Claude model while only the Claude adapter exists); `BRIEFING_TIME` is when the Briefing is written and due by, `HH:MM` in `TZ`; `GOSLOW_ANTHROPIC_API_KEY` is a `config.Secret`, which never formats as its value. Never print, log or commit a real key.

## Go toolchain

- **Go is pinned by the `go` directive in `go.mod`** (1.27.3), the same in `tools/go.mod`. A local Go that is older downloads the pinned toolchain itself (`GOTOOLCHAIN=auto`, the default), CI's `actions/setup-go` reads `go.mod`, and the `Dockerfile`'s `golang:` tag repeats the version. Upgrade all four in one PR.
- **Tools are pinned in their own module**, `tools/go.mod` (`tool` directive), and run with `go tool -modfile=tools/go.mod <tool>`. A separate module keeps golangci-lint's hundreds of dependencies out of the app's `go.mod` and build. golangci-lint is at v2.14.0; its config is `.golangci.yml` (the `standard` linters plus a few for HTTP, SQL and errors; gofmt and goimports as formatters).
- **SQLite is `modernc.org/sqlite`**, pure Go, so the binary builds with `CGO_ENABLED=0` and runs on a distroless static image. Migrations are embedded SQL files in `internal/sqlite/migrations`, applied in name order by `sqlite.Open`, with the count applied kept in `PRAGMA user_version`. Never edit a migration that has been merged; add the next one.
- **htmx is vendored** at `internal/web/static/htmx-2.0.10.min.js`, byte-identical to the npm release (sha256 `71ea6718…c0de`), and excluded from the whitespace hooks so it stays so. Upgrading is replacing the file and the `<script>` in `templates/layout.html`. It stays on htmx 2.x while the Web apps Guide is written against it; htmx 4 is a separate decision.

## Checks and CI

- pre-commit hooks: the Global Guide's (`pre-commit-hooks`, actionlint, shellcheck), then `go mod tidy -diff` for both modules, `golangci-lint fmt`, `golangci-lint run` and `go test -race ./...`. The Go hooks are `language: system`, so they need `go` on PATH.
- CI is `.github/workflows/ci.yml`: one job, **`pre-commit`**, on every PR, and on pushes to `main` that change `go.mod`, `go.sum`, `tools/` pins or `.pre-commit-config.yaml`. **The required status check is `pre-commit`.** Renaming the job breaks merging once the `main` ruleset exists (it is created after CI first reports under this name).
- Actions and images are pinned to exact versions (the distroless base by digest).

## Known traps

- **Never name go-slow's key `ANTHROPIC_API_KEY`.** Claude Code reads that variable too: set in the shell `claude` is launched from, it switches Claude Code from the claude.ai login to the key, for its own requests and billing, and claude.ai connectors stop working. go-slow reads `GOSLOW_ANTHROPIC_API_KEY` instead, which agents inherit without Claude Code taking it. *Remove when:* never, while go-slow is built with Claude Code.
- actionlint shellchecks workflow `run:` steps only when `shellcheck` is on PATH. It is on GitHub's runners, not necessarily locally, so a local run can pass a step CI fails. *Remove when:* the actionlint hook brings its own shellcheck.
