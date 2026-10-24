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
  - `internal/editor`: the Editor. Its acts (so far *reading an Item* into Facts, Claims and Entities), each with its own instructions, output schema and checks, and the provider-neutral model port (`Model`) they call. Collection runs reading after the check, over the Items tied to a Topic and not yet read; a reading that fails its checks writes nothing and is retried on the next run.
  - `internal/claude`: the Claude model adapter (Anthropic's Messages API through `anthropic-sdk-go`, structured output against the act's schema). Anthropic's types stay inside it.
  - `internal/polite`: the HTTP adapter Collection fetches through, carrying the politeness rules (User-Agent, conditional requests, one request per host at a time, `Retry-After` backoff, daily robots.txt).
  - `internal/sqlite`: the storage adapter, with its migrations.
  - `internal/server`: serving with graceful shutdown.

## Commands

- **Inner loop:** `go test ./...` (add `-run <Test>` or a package path to narrow it).
- **Full check:** `uvx --from pre-commit==4.7.0 pre-commit run --all-files`. It runs every hook below; CI runs exactly this. A `pre-commit` already on PATH works too, if it is at least 4.5.0. Install the git hook once per clone with `uvx --from pre-commit==4.7.0 pre-commit install`.
- **Re-record the Editor's recorded runs** (deliberately, when an act's instructions or schema change; it calls the real model once per fixture and costs money): `go test -count=1 ./internal/editor -run TestRecordedReadings -record`, with `ANTHROPIC_API_KEY` and `READING_MODEL` set in the environment. Target the package: other packages don't define `-record`. The responses land in `internal/editor/testdata/recorded/`; commit them, and the diff shows what changed. The suite only replays them and never calls a model. **The first recording has not been made yet** (no API key was available when the harness landed): until it is, the replay test skips each fixture with a message naming this command. Fixtures (`internal/editor/testdata/govuk/`) come only from admissible Sources, with their licence attribution.
- **Lint and format:** `go tool -modfile=tools/go.mod golangci-lint run ./...` and `... golangci-lint fmt`.
- **Run locally:** `cp .env.example .env`, then `docker compose up --build`, and open http://localhost:8080. The app logs JSON lines to stdout and stops cleanly on SIGTERM (`docker compose stop`). Outside docker: `go run ./cmd/go-slow` with the variables in `.env.example` set.

## Config

Only from environment variables, every one listed with a safe local value in `.env.example`, and all required: a missing one fails at start. The real `.env` is gitignored. Add a variable to `internal/config` and `.env.example` together. The model each act runs on is a variable (`READING_MODEL`, a Claude model while only the Claude adapter exists); `ANTHROPIC_API_KEY` is a `config.Secret`, which never formats as its value. Never print, log or commit a real key.

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

- actionlint shellchecks workflow `run:` steps only when `shellcheck` is on PATH. It is on GitHub's runners, not necessarily locally, so a local run can pass a step CI fails. *Remove when:* the actionlint hook brings its own shellcheck.
