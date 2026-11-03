# The Editor's recorded runs

The responses the real model gave the Editor's acts on committed fixtures, which the suite replays (tier 2 of the Model calls Guide's *three tiers*). The commands that record them, with their environment variables, costs and gates, are in the repo's `CLAUDE.md` (*Commands*). The approach is the Model calls Guide's, in its detail files under `~/.claude/commons/guides/model-calls/`: `recording.md` (a recording answers one request), `samples.md` (an act is judged over its samples), `changing-an-act.md` (frozen copies, refresh, probing on one sample) and `fixtures-and-properties.md`. Why go-slow's acts are recorded so is the system design's *The Editor* (*A recording is one sample, and a property is judged by its pass rate*). This file is go-slow's own detail: what is where, and what each act's recording asserts.

## What is where

- `<act>/<fixture>/<k>.json`: sample `k` (from 1) of a fixture, the sample's last attempt, with the attempts the act's checks refused before it under `earlier_attempts`, written attempt by attempt as it arrives. Acts: `reading`, `relating`, `briefing` (one fixture, `saturday`), `precedent` (one directory per case) and `sunday` (`sunday/<k>/try-<n>/`, below).
- `<act>/inputs/<upstream act>/<fixture>.json`: the act's own frozen copies of the recordings it rests on (relating over readings; the Briefing over readings and relatings; the precedent cases over readings and relatings, copied from the Briefing's; the Sunday read over readings, relatings and the Briefing, `sunday/inputs/briefing/saturday.json`). Each act is replayed over these, never over the upstream recordings themselves, so re-recording one act leaves every other act valid: re-record only the act you changed. Frozen copies are replayed unchecked, since the act that made them checks them.
- The fixtures are `../govuk/`, only from admissible Sources, with their licence attribution.
- Commit the recordings: the diff shows what changed. The suite only replays them and never calls a model. Target the package when recording (`./internal/editor`): other packages don't define `-record`.

## Samples, and how they are judged

- Each fixture is sampled 5 times (`dailyRule`, `recorded_samples_test.go`); the Sunday read 3 times (`sundayReadRule`). Each sample runs as in use, up to `editor.MaxAttempts` attempts against the act's checks.
- Only an answer that came back counts against a sample: one the act's checks refuse, or one the adapter returns as an error with the response it came in (a refusal, `editor.ErrRefused`; an answer cut off, `editor.ErrTruncated`). That is the act's behaviour on its request, a failed attempt in use, kept as an attempt with its `error` (and `"output": null`) and replayed as the same error, its request hash checked as any attempt's (the Sunday read keeps such a turn with its error the same way).
- A live call that answers nothing (an API error after the adapter's own retries, with no response) is no attempt, and is made again, up to `liveTries` (3) calls `livePause` (30s) apart; if none answers, the recording stops with an error wrapping `errLost` and the sample (for the Sunday read, the try) is not kept, so recording again resumes it.
- The suite replays every sample and judges each property per fixture: *passes its checks within its attempts* over all the samples, each test-only property over the samples that passed. Daily acts: supported at 5 of 5, refuted at 3 or fewer, undecided at 4. The Sunday read: supported at 3 of 3, refuted at 1 or none, undecided at 2. Refuted or undecided fails the test, as does a fixture with fewer samples than the rule, or samples answering different requests.
- An undecided fixture is read again on fresh samples, judged alone: delete its directory (for the Sunday read, `sunday/<k>/`) and record again. `-record` keeps the samples already kept for the request the act sends now, so it also resumes a stopped recording, and draws only what is missing or stale. Don't add samples to push a count over the line, and don't change a check or property to pass.
- Each recording keeps the hash of the request it answered (`editor.RequestHash`, with the model the act is pinned to in `.env.example`); a replay whose act now builds another request (changed instructions, schema, input or pin) fails as stale, naming the command that re-records it. A fixture with no samples fails with a message naming the command too.

## What each act's recording asserts

### Reading

Each fixture is shown as the sentence cutter cuts it (`fixtureToRead`). Its properties are judged per fixture where the fixture carries the case (asserted present first):

- a bare "X said:" Span is Rejected (`bareAttribution`);
- each Span of a quotation is given as its speaker who the Item names before it (`quotations`);
- a pronoun refers to its antecedent and "the city" to Derby (`references`);
- a named referring expression is quoted as only its words, and its clear target as only its name or noun phrase (`expressions`: "it" in the US–Russia response, "it" in the Scotland speech's "If it’s not based on that, it won’t work.", "the city" to "Derby"), wherever it is cited;
- the readout naming the Prime Minister only by office names a person with that office.

### Relating

Relating reads its frozen readings, looks their names up in a Wikidata that finds nothing (`noMatch`, so each name is the Editor's own Entity and no recording calls the network), and relates each fixture in the order they were published. Its fixtures build on each other: each sample of a fixture runs over the Record the fixtures before it left, each carried on by its first sample to pass, so a fresh reading of one fixture makes the later fixtures' samples stale and `-record` redraws them. A fixture whose Spans find no candidates (the first, with nothing read before it) makes no call, and its samples keep none (`"output": null`).

Its properties are judged on the Holyrood speech: it *follows* the press release that said what the Prime Minister *will* do, and its "most centralised" line is *same as* the release's and not corroboration (one publisher).

### The Briefing

It replays its own copies of the readings and relatings (`briefing/inputs/{reading,relating}/`, built by `spanRecordFile`, names looked up in `noMatch`) into a Record, and writes the day's Briefing over every Span read (`briefing/saturday/`). Its properties, judged over the samples that pass the checks, with each case asserted present in what it is shown first (`casesPresentForBriefing`):

- it tells something;
- a statement citing a Span with a speaker names them, by a word of how they are shown (`namesSpeaker`);
- a statement telling the railways headquarters' "£151 million boost" tells it as a projection (`projectionMarker`);
- the named duplicate is told once: the Holyrood speech's "most centralised" line and the press release's, asserted present and related *same as* first (`duplicateWords`), are never told one in a statement and the other in another (telling one and leaving the other untold is telling it once).

Told once is asserted on that pair alone because relating's *same as* is loose on generic lines ("for as long as it takes"): asserted on every *same as* pair it was refuted, and the stricter "a statement citing one cites both" more so, so those pairs are read in use, through the Owner's Feedback (system design: *The Briefing tells what the Editor judges worth telling*). Iterate on its instructions on single samples first, and look at them, before recording.

### The precedent cases

Each is one case with one piece of hand-made Feedback standing, over the precedents' own frozen copies (`precedent/inputs/{reading,relating}/`, copied from the Briefing's), sampled by `dailyRule` and kept at `precedent/<case>/`, and asserts first that the act is shown that Feedback:

- relating the Commons statement again with *wrong* on its "In the three weeks since that meeting…" same as the response's "Ukraine has repeatedly demonstrated…": that pair is not judged so again; its "Rejecting aggression…" is still the same as the response's "committed to supporting Ukraine…";
- Saturday's Briefing over every fixture with *unwanted* on the County Tyrone appointment as a hand-made Friday Briefing told it: the appointment isn't told; the Briefing tells something.

Each case is the one the Feedback was given on, judged again: on probes, one precedent carried to no similar pair (another Crown appointment, another Span the same as the same one). Reading has no precedent case: it is shown none.

### The Sunday read

Each sample runs as in use, up to `SundayReadTries` tries (`-max-tries N` bounds them lower), each a whole conversation over a fresh copy of a Record built from its own copies of the readings and relatings, with Saturday's Briefing written over them from its copy (`sunday/inputs/`, built by `toldRecordFile`, so re-recording the earlier acts leaves its replay valid). The cases its tools work on are asserted present first (`casesPresentForSundayRead`: a Span told with relations, and one naming an Entity). Its properties:

- it says something;
- every statement cites what exists (Spans within their Items' text that reading did not reject, Assessments resting on Spans, lookups naming a revision);
- every Assessment rests on Spans that exist, as at the read's day.

Each try is kept as it runs, turn by turn, at `sunday/<k>/try-<n>/conversation.json` (every turn in the provider's own form, with `editor.ConversationRequestHash` of its request, the hash a kept turn carries), with Wikidata's answers to its lookups beside it (`wikidata/`, fetched with a placeholder contact), so the replay makes no network call. A turn whose request the act no longer sends fails the replay as stale; `-record` keeps the tries that still replay and draws afresh a stale one or one left unfinished.

## Refreshing an act's frozen copies

A refresh brings a downstream act onto an upstream act's newer recordings. It is its own change, never part of the change to the upstream act, made once that act's new behaviour is accepted on its own recordings.

- Copy every upstream act's current recordings together over the act's `inputs/`: a frozen copy is the fixture's first sample, copied as it is (for relating, each `reading/<fixture>/1.json` to `relating/inputs/reading/<fixture>.json`; for the Sunday read also `briefing/saturday/1.json` to `sunday/inputs/briefing/saturday.json`, in its own commit), and only from recordings whose every property is supported, so the first sample's last attempt is the one that passed. Then re-record that act alone.
- Never copy one upstream act without the others: new readings with old relations is a run no Editor makes.
- **Refreshes go in chain order** (relating, then the Briefing, then the Sunday read). An act's recordings answer its own frozen copies, so relating's current recordings answer the readings in `relating/inputs/reading/`, and fit reading's current recordings only once relating has been refreshed onto them and re-recorded. Until then, copying reading's and relating's current recordings together over the Briefing's `inputs/` is the run no Editor makes, its relations naming candidates by numbers another reading offered.
- Refresh the Sunday read only on the Owner's say, since it costs dollars.

Until a refresh, two checks cover the gap, with no recording of their own:

- **The chain replay** (`TestRecordedChainTracesEveryBriefingStatementToSpansNotRejected`), over the Briefing's copies and each of its samples that passes: every statement cites Spans in its fixture's text that reading did not reject.
- **The compatibility check** (`TestEachActsCurrentRecordingsAreTakenByTheActsDownstream`):
  - reading's current recordings, each fixture's first sample, fed through relating on a fake model, which must be asked about every fixture relating's own recordings were asked about, its answers passing the checks (negative case: `TestTheCompatibilityCheckFailsOnAReadingRelatingCannotTake`);
  - then reading's through relating and the Briefing, each on a fake model (`feedBriefing`). Relating's recorded answers are not replayed there, since they name candidates by number as the one reading they were recorded over offered them, and a fresh reading may offer others (`TestTheCompatibilityCheckTakesReadingsOfferingOtherCandidates`);
  - then relating's current recordings, each fixture's first sample, replayed over the readings they were recorded over (relating's own frozen copies, so their candidate numbers line up) and the Briefing written over them on a fake model (`feedRelatingToBriefing`; negative cases `TestTheCompatibilityCheckFailsOnARelatingTheBriefingCannotTake` and `TestTheCompatibilityCheckFailsOnRelatingsRelatingNothing`).
  - The Briefing's fake tells each Span shown with relations, its Story gathered from every Span they reach, so the Briefing's checks see every relation, and the check fails if it is shown none. The Sunday read is not fed through it, its recordings costing dollars to refresh.

Each downstream act's own recorded test replays over its own directory copied alone (`ownDirectory`, the Sunday read's included), so an act that reads another's recordings fails.

## Test speed

A frozen Record is built once per test binary for the bytes of the fixtures and copies it is built from (`frozenOnce`), so the Briefing, the chain replay and the Sunday read share one build of byte-identical copies, each given its own copy. The recorded tests run in parallel (`t.Parallel`). Every test's Record is a copy of one migrated once per test binary (`internal/sqlite/sqlitetest`; the storage adapter's own tests, `openTestStore`), since under `-race` migrating takes about a second. Time the package when adding a recorded test.

## The yield report

Every re-recording reports each act's yield, pasted into the PR body. Extract the recordings being replaced (e.g. `git archive -o <scratch>/old.tar origin/main internal/editor/testdata/recorded`, then `tar -xf <scratch>/old.tar -C <scratch>/old`), then run `go run ./internal/editor/yield <scratch>/old/internal/editor/testdata/recorded internal/editor/testdata/recorded`.

It prints, per act and per fixture, before → after: for reading, the Spans the fixture is cut into (from the fixtures beside either directory, `../govuk`), and those Rejected, the names, speakers and *refers to* it judged; relations by kind (*follows*, *same as*, *contradicts*) for relating, a sample with no call counting none; for the Briefing, its statements, the distinct Spans they cite and the Stories it gathered (those the model gathered), with totals, and marks fixtures added or removed. A fixture's samples show as the range they drew, least–most, each by its last attempt, and a total as the range from every fixture's least to its most. It reports and never fails: whether a shift is a loss is a reading. It calls no model, counts nothing in `inputs/` (frozen copies of upstream recordings) and acts it doesn't count (the Sunday read), and is a dev tool under `internal/`, not part of the app binary.
