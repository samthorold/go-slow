# The Editor's recorded runs

The responses the real model gave the Editor's acts on committed fixtures, which the suite replays (tier 2 of the Model calls Guide's *three tiers*). The commands that record them, with their environment variables, costs and gates, are in the repo's `CLAUDE.md` (*Commands*). The approach is the Model calls Guide's, in its detail files under `~/.claude/commons/guides/model-calls/`: `recording.md` (a recording answers one request), `samples.md` (an act is judged over its samples), `changing-an-act.md` (frozen copies, refresh, probing on one sample) and `fixtures-and-properties.md`. Why go-slow's acts are recorded so is the system design's *The Editor* (*A recording is one sample, and a property is judged by its pass rate*). This file is go-slow's own detail: what is where, and what each act's recording asserts.

## What is where

- `<act>/<fixture>/<k>.json`: sample `k` (from 1) of a fixture, the sample's last attempt, with the attempts the act's checks refused before it under `earlier_attempts`, written attempt by attempt as it arrives. Acts: `hydration`, `weaving` (two runs, `to-15-october` and `16-october`, below) and `naming` (each Story weaving's frozen copies make, `<run>-story-<n>`, below).
- `<act>/inputs/<upstream act>/<fixture>.json`: the act's own frozen copies of the recordings it rests on. Each act is replayed over these, never over the upstream recordings themselves, so re-recording one act leaves every other act valid: re-record only the act you changed. Frozen copies are replayed unchecked, since the act that made them checks them. Weaving's are `weaving/inputs/hydration/`: it is shown the Stories the fixtures' Cuts made (the sentence cutter is plain code), each Span hydrated by its Item's hydration, so every fixture Item is hydrated from its copy before a run, and a missing copy, or one hydration's checks refuse, fails the run. Naming's are `naming/inputs/weaving/<run>.json` (each weaving run's first sample) and `naming/inputs/hydration/` (the hydrations those samples were recorded over, since a weaving answer cites Spans by the numbers its hydrated input gave them): weaving's two runs are replayed from them in order to make the Stories naming names, and a missing copy, or one weaving's checks refuse, fails it. A copy of a conversation is replayed through the batch runner, every call in order; a copy recorded before a run was a conversation (weaving's version 3, which keeps no turn as it came back: today's copies) is kept as version 3 numbered what it was shown, every Story the Editor had made and then the Stories cut since (`editor.KeepAsWeavingVersion3`, test-only), until a refresh brings naming onto weaving's current recordings.
- The compatibility check (`recorded_compatibility_test.go`, `TestEachActsCurrentRecordingsAreTakenByTheActsDownstream`) feeds each act's current recordings (each fixture's first sample) to the act downstream on a fake model. Hydration's to weaving: every Item must be hydrated, weaving's request must build and show it, and the fake's answer must pass weaving's checks. Weaving's, over the hydrations they were recorded on, to naming: each run's recording must be taken by weaving's checks, and every Story it makes named by a fake whose answer passes naming's. A change to what an act writes that the act downstream can't take fails there at once; a change in what it judges is brought downstream by a refresh.
- The fixtures are `../govuk/`, only from admissible Sources, with their licence attribution.
- Commit the recordings: the diff shows what changed. The suite only replays them and never calls a model. Target the package when recording (`./internal/editor`): other packages don't define `-record`.

## Samples, and how they are judged

- Each fixture is sampled 5 times (`dailyRule`, `recorded_samples_test.go`), weaving's runs included. `-max-samples N` bounds the samples recording draws of each fixture, to probe on one before paying for the rule's. Each sample runs as in use, up to `editor.MaxAttempts` attempts against the act's checks.
- Only an answer that came back counts against a sample: one the act's checks refuse, or one the adapter returns as an error with the response it came in (a refusal, `editor.ErrRefused`; an answer cut off, `editor.ErrTruncated`). That is the act's behaviour on its request, a failed attempt in use, kept as an attempt with its `error` (and `"output": null`) and replayed as the same error, its request hash checked as any attempt's.
- A live call that answers nothing (an API error after the adapter's own retries, with no response) is no attempt, and is made again, up to `liveTries` (3) calls `livePause` (30s) apart; if none answers, the recording stops with an error wrapping `errLost` and the sample is not kept, so recording again resumes it.
- The suite replays every sample and judges each property per fixture: *passes its checks within its attempts* over all the samples, each test-only property over the samples that passed. Hydration and weaving (`dailyRule`): supported at 5 of 5, refuted at 3 or fewer, undecided at 4. Refuted or undecided fails the test, as does a fixture with fewer samples than the rule, or samples answering different requests.
- An undecided fixture is read again on fresh samples, judged alone: delete its directory and record again. `-record` keeps the samples already kept for the request the act sends now, so it also resumes a stopped recording, and draws only what is missing or stale. Don't add samples to push a count over the line, and don't change a check or property to pass.
- Each recording keeps the hash of the request it answered (`editor.RequestHash`, with the model the act is pinned to in `.env.example`; for a conversation's turn, its tools and the conversation so far too); a replay whose act now builds another request (changed instructions, schema, tools, input or pin, or for a later turn an ask answered otherwise) fails as stale, naming the command that re-records it. A fixture with no samples fails with a message naming the command too.

## What each act's recording asserts

### Hydration

Each fixture is shown as the sentence cutter cuts it (`fixtureToHydrate`). Its properties are judged per fixture where the fixture carries the case (asserted present first):

- a bare "X said:" Span is Rejected (`bareAttribution`);
- each Span of a quotation is given as its speaker who the Item names before it (`quotations`);
- a pronoun refers to its antecedent and "the city" to Derby (`references`);
- a named referring expression is quoted as only its words, and its clear target as only its name or noun phrase (`expressions`: "it" in the US–Russia response, "it" in the Scotland speech's "If it’s not based on that, it won’t work.", "the city" to "Derby"), wherever it is cited;
- the readout naming the Prime Minister only by office names a person with that office.

### Weaving

A run is a conversation: its first turn is shown only the Stories new to it, and it reaches earlier Stories by asking (`find` by words, `open_story`), each turn a request of its own. A sample's file keeps every call of the sample in the order made, turns and refused attempts alike: its last attempt is the answer, and the turns before it are `earlier_attempts`, each with the request hash of its own turn, the tools it called (`calls`), its text (`text`) and the turn as the provider returned it (`raw`), since the next turn sends it back and so its request is rebuilt from it. Only the first turn's request is the same in every sample; each later turn's is its sample's own.

Two runs a day apart, over the fixture Items in the order published (`weavingRuns`), each Item hydrated by its frozen copy of hydration's recording: Saturday's, `to-15-october`, shown the Stories the Cuts of the Items published up to 15 October made, with nothing earlier in the Record, so its asks find nothing; Sunday's, `16-october`, shown the speech's Story, its asks answered from the earlier Record Saturday's left: Saturday's Cut Stories (some bearing on the speech, some not: the Lord-Lieutenant of County Tyrone) and the Stories Saturday's run made, carried on by its first sample to pass its checks. That earlier Record is frozen with the fixtures: it is made only from committed recordings, so every answer, and so every turn's request, is the same on every replay. Sunday's run asserts first that the earlier Record holds a Story the Editor made, so a fresh recording of Saturday's whose first passing sample makes no Story fails it. A fresh recording of Saturday's makes Sunday's samples stale. Each sample runs as in use, through the batch runner, its batches answered at once (`modelBatcher`), every turn and up to `editor.MaxAttempts` attempts at each against the act's checks, at most `maxTurns` turns of the runner. Its properties beside the checks:

- every citation of every Story the run makes is a Span the run was shown, never one Rejected and left out, with its words as written: one of the new Stories' (`shownAsNew`, read from the Record rather than the request), or of an earlier Story an ask's answer named (`shownByAsks`, from the conversation its kept calls hold; looser than what a catalogue numbered, since it takes every Span of a Story named);
- at least one sample of Sunday's run reaches an earlier Story through an ask (an answer names one), judged over the run's samples, not per sample.

Nothing asserts what an opinion says, or that a run makes any Story: that is read in use (system design: *Weaving asks for an editor's opinion, and need get none*).

### Naming

Each Story weaving's frozen copies make is a fixture, `<run>-story-<n>`, numbered in the order the run wrote them; each sample names that Story alone, over a copy of the Record the copies make, through the batch runner (`modelBatcher`), up to `editor.MaxAttempts` against the act's checks. A fresh copy of weaving's recordings (a refresh) makes every sample stale, since the Story's words are naming's input. Its one property beside the checks, over the samples that passed them:

- its Name is no other Story's: no Name a sample gives its Story is a Name any sample gives another Story, the same words whatever their case or spacing (`namesDiffer`, `sameName`).

Nothing asserts that a Name uses only its Story's words: a Name is only for the Owner to recognise a Story by in a list, so any plain subject will do.

## Refreshing an act's frozen copies

A refresh brings a downstream act onto an upstream act's newer recordings. It is its own change, never part of the change to the upstream act, made once that act's new behaviour is accepted on its own recordings.

- Copy every upstream act's current recordings together over the act's `inputs/`: a frozen copy is the fixture's first sample, copied as it is, and only from recordings whose every property is supported, so the first sample's last attempt is the one that passed. Then re-record that act alone.

## Test speed

The recorded tests run in parallel (`t.Parallel`). Every test's Record is a copy of one migrated once per test binary (`internal/sqlite/sqlitetest`; the storage adapter's own tests, `openTestStore`), since under `-race` migrating takes about a second. Time the package when adding a recorded test.

## The yield report

Every re-recording reports each act's yield, pasted into the PR body. Extract the recordings being replaced (e.g. `git archive -o <scratch>/old.tar origin/main internal/editor/testdata/recorded`, then `tar -xf <scratch>/old.tar -C <scratch>/old`), then run `go run ./internal/editor/yield <scratch>/old/internal/editor/testdata/recorded internal/editor/testdata/recorded`.

It counts hydration, weaving and naming, and prints, per act and per fixture, before → after: for hydration, the Spans the fixture is cut into (from the fixtures beside either directory, `../govuk`), and those Rejected, the names, speakers and *refers to* it judged; for weaving, the Stories it made, their Editor's Spans and the distinct Spans they cite; for naming, the words of the Name it gave the Story (none where the last attempt gave none); with totals, and marks fixtures added or removed. A fixture's samples show as the range they drew, least–most, each by its last attempt, and a total as the range from every fixture's least to its most. It reports and never fails: whether a shift is a loss is a reading. It calls no model and counts nothing in `inputs/` (frozen copies of upstream recordings). Recordings of any other act (a retired act's, such as relating's or the Briefing's in an older tree) it doesn't count, and names those acts on a last line, *Not counted*. An older tree's recordings of hydration under its earlier name, reading (`reading/`, act `reading`), it counts as hydration's. It is a dev tool under `internal/`, not part of the app binary.
