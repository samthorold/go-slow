# The Editor's recorded runs

The responses the real model gave the Editor's acts on committed fixtures, which the suite replays (tier 2 of the Model calls Guide's *three tiers*). The commands that record them, with their environment variables, costs and gates, are in the repo's `CLAUDE.md` (*Commands*). The approach is the Model calls Guide's, in its detail files under `~/.claude/commons/guides/model-calls/`: `recording.md` (a recording answers one request), `samples.md` (an act is judged over its samples), `changing-an-act.md` (frozen copies, refresh, probing on one sample) and `fixtures-and-properties.md`. Why go-slow's acts are recorded so is the system design's *The Editor* (*A recording is one sample, and a property is judged by its pass rate*). This file is go-slow's own detail: what is where, and what each act's recording asserts.

## What is where

- `<act>/<fixture>/<k>.json`: sample `k` (from 1) of a fixture, the sample's last attempt, with the attempts the act's checks refused before it under `earlier_attempts`, written attempt by attempt as it arrives. Acts: `reading`.
- `<act>/inputs/<upstream act>/<fixture>.json`: the act's own frozen copies of the recordings it rests on. Each act is replayed over these, never over the upstream recordings themselves, so re-recording one act leaves every other act valid: re-record only the act you changed. Frozen copies are replayed unchecked, since the act that made them checks them.
- The fixtures are `../govuk/`, only from admissible Sources, with their licence attribution.
- Commit the recordings: the diff shows what changed. The suite only replays them and never calls a model. Target the package when recording (`./internal/editor`): other packages don't define `-record`.

## Samples, and how they are judged

- Each fixture is sampled 5 times (`dailyRule`, `recorded_samples_test.go`). Each sample runs as in use, up to `editor.MaxAttempts` attempts against the act's checks.
- Only an answer that came back counts against a sample: one the act's checks refuse, or one the adapter returns as an error with the response it came in (a refusal, `editor.ErrRefused`; an answer cut off, `editor.ErrTruncated`). That is the act's behaviour on its request, a failed attempt in use, kept as an attempt with its `error` (and `"output": null`) and replayed as the same error, its request hash checked as any attempt's.
- A live call that answers nothing (an API error after the adapter's own retries, with no response) is no attempt, and is made again, up to `liveTries` (3) calls `livePause` (30s) apart; if none answers, the recording stops with an error wrapping `errLost` and the sample is not kept, so recording again resumes it.
- The suite replays every sample and judges each property per fixture: *passes its checks within its attempts* over all the samples, each test-only property over the samples that passed. Daily acts: supported at 5 of 5, refuted at 3 or fewer, undecided at 4. Refuted or undecided fails the test, as does a fixture with fewer samples than the rule, or samples answering different requests.
- An undecided fixture is read again on fresh samples, judged alone: delete its directory and record again. `-record` keeps the samples already kept for the request the act sends now, so it also resumes a stopped recording, and draws only what is missing or stale. Don't add samples to push a count over the line, and don't change a check or property to pass.
- Each recording keeps the hash of the request it answered (`editor.RequestHash`, with the model the act is pinned to in `.env.example`); a replay whose act now builds another request (changed instructions, schema, input or pin) fails as stale, naming the command that re-records it. A fixture with no samples fails with a message naming the command too.

## What each act's recording asserts

### Reading

Each fixture is shown as the sentence cutter cuts it (`fixtureToRead`). Its properties are judged per fixture where the fixture carries the case (asserted present first):

- a bare "X said:" Span is Rejected (`bareAttribution`);
- each Span of a quotation is given as its speaker who the Item names before it (`quotations`);
- a pronoun refers to its antecedent and "the city" to Derby (`references`);
- a named referring expression is quoted as only its words, and its clear target as only its name or noun phrase (`expressions`: "it" in the US–Russia response, "it" in the Scotland speech's "If it’s not based on that, it won’t work.", "the city" to "Derby"), wherever it is cited;
- the readout naming the Prime Minister only by office names a person with that office.

## Refreshing an act's frozen copies

A refresh brings a downstream act onto an upstream act's newer recordings. It is its own change, never part of the change to the upstream act, made once that act's new behaviour is accepted on its own recordings.

- Copy every upstream act's current recordings together over the act's `inputs/`: a frozen copy is the fixture's first sample, copied as it is, and only from recordings whose every property is supported, so the first sample's last attempt is the one that passed. Then re-record that act alone.

## Test speed

The recorded tests run in parallel (`t.Parallel`). Every test's Record is a copy of one migrated once per test binary (`internal/sqlite/sqlitetest`; the storage adapter's own tests, `openTestStore`), since under `-race` migrating takes about a second. Time the package when adding a recorded test.

## The yield report

Every re-recording reports each act's yield, pasted into the PR body. Extract the recordings being replaced (e.g. `git archive -o <scratch>/old.tar origin/main internal/editor/testdata/recorded`, then `tar -xf <scratch>/old.tar -C <scratch>/old`), then run `go run ./internal/editor/yield <scratch>/old/internal/editor/testdata/recorded internal/editor/testdata/recorded`.

It prints, per act and per fixture, before → after: for reading, the Spans the fixture is cut into (from the fixtures beside either directory, `../govuk`), and those Rejected, the names, speakers and *refers to* it judged; relations by kind (*follows*, *same as*, *contradicts*) for relating, a sample with no call counting none; for the Briefing, its statements, the distinct Spans they cite and the Stories it gathered (those the model gathered), with totals, and marks fixtures added or removed. A fixture's samples show as the range they drew, least–most, each by its last attempt, and a total as the range from every fixture's least to its most. It reports and never fails: whether a shift is a loss is a reading. It calls no model, counts nothing in `inputs/` (frozen copies of upstream recordings) and acts it doesn't count (the Sunday read), and is a dev tool under `internal/`, not part of the app binary.
