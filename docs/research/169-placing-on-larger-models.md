# How placing differs on Sonnet 5.5 and Opus 5.5 (#169)

A measurement, not a recommendation: what placing does on `claude-sonnet-5-5` and `claude-opus-5-5`, set beside what it does on `claude-haiku-5-5`, the model it is pinned to. Placing's checks test structure, not judgement, and too little Feedback has been given to read judgement from it. So this puts the three models side by side on the same requests.

## How it was measured

- **Same requests, bar the model.** Each of placing's 8 fixtures was sampled 5 times on each model, as `-record` does (each sample up to `editor.MaxAttempts` attempts), over placing's frozen readings. As a throwaway change to the harness, each fixture's samples ran over the Record that **Haiku's** committed first samples left, not the model's own. Every request is then Haiku's request with only the model changed: each fixture's input had the same token count on all three models. In use a larger model would build its own Stories, so on later fixtures it would be offered other candidates and the differences would compound. This measures each placing judgement on its own, not a run of the Editor.
- **No recording was committed** and `.env.example`'s pin did not change. Haiku's samples are the ones committed under `internal/editor/testdata/recorded/placing/`.
- **Every sample passed its checks on its first attempt**, on both models: 80 calls, no refusals, no retries. No fixture failed on any sample.
- **Spend:** about $0.40 on Sonnet (135,855 input tokens at $2/MTok, 12,894 output at $10/MTok) and $0.98 on Opus (135,855 input at $4/MTok, 21,669 output at $20/MTok). That is about $1.38 of the $10 budget, at the prices on Anthropic's pricing page on 2026-10-28.

**Agreement** below is per Fact. Two samples agree on a Fact when they place it identically: in the same candidate Stories, in a new Story with the same other Facts, or nowhere. New Stories are matched by which Facts they hold, not by their titles or refs. *Within* a model is the mean over its 10 pairs of samples; *between* two models it is the mean over their 25 cross pairs. Both are averaged over a fixture's Facts, then over the fixtures. A Fact's *modal* placing is its most common placing across a model's 5 samples.

## Yield

`go run ./internal/editor/yield` with Haiku's committed recordings as *old*. Each cell is the range over the 5 samples.

**Haiku → Sonnet**

| Fixture | Developments | New Stories |
|---|---|---|
| appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026 | 2 → 2 | 1 → 1 |
| great-british-railways-takes-shape-as-hq-and-apprenticeship-scheme-confirmed | 6–8 → 6–7 | 1–5 → 2 |
| pm-meeting-with-president-zelenskyy-of-ukraine-14-october-2026 | 2–3 → 3–4 | 0 → 0 |
| pm-speech-in-scotland-16-october-2026 | 7–9 → 6–7 | 1–4 → 0 |
| pm-statement-to-the-house-of-commons-12-october-2026 | 14–15 → 12–15 | 9–11 → 8–9 |
| president-zelenskyy-to-visit-downing-street | 6–7 → 6–7 | 0 → 0 |
| prime-minister-makes-offer-to-holyrood-to-work-together-to-empower-scottish-regions | 3 → 3 | 1 → 1 |
| uk-government-response-to-agreement-between-us-and-russia-9-october | 0 → 0 | 0 → 0 |
| **Total** | 40–47 → 38–45 | 13–22 → 12–13 |

**Haiku → Opus**

| Fixture | Developments | New Stories |
|---|---|---|
| appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026 | 2 → 2 | 1 → 1 |
| great-british-railways-takes-shape-as-hq-and-apprenticeship-scheme-confirmed | 6–8 → 2–3 | 1–5 → 1–2 |
| pm-meeting-with-president-zelenskyy-of-ukraine-14-october-2026 | 2–3 → 2 | 0 → 0 |
| pm-speech-in-scotland-16-october-2026 | 7–9 → 4–5 | 1–4 → 0–1 |
| pm-statement-to-the-house-of-commons-12-october-2026 | 14–15 → 11–12 | 9–11 → 8–9 |
| president-zelenskyy-to-visit-downing-street | 6–7 → 7–8 | 0 → 0 |
| prime-minister-makes-offer-to-holyrood-to-work-together-to-empower-scottish-regions | 3 → 2–3 | 1 → 1 |
| uk-government-response-to-agreement-between-us-and-russia-9-october | 0 → 0 | 0 → 0 |
| **Total** | 40–47 → 30–35 | 13–22 → 11–14 |

By modal placing over the 93 Facts, Haiku makes 42 Facts Developments (29 of them in new Stories), Sonnet 42 (25) and Opus 32 (20).

## Agreement: sampling noise against model difference

| Fixture | Haiku within | Sonnet within | Opus within | Haiku–Sonnet | Haiku–Opus | Sonnet–Opus |
|---|---|---|---|---|---|---|
| appointment-of-lord-lieutenant… | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 |
| great-british-railways… | 0.43 | 0.82 | 0.82 | 0.45 | 0.42 | 0.52 |
| pm-meeting-with-president-zelenskyy… | 0.91 | 0.91 | 1.00 | 0.78 | 0.94 | 0.77 |
| pm-speech-in-scotland… | 0.82 | 0.96 | 0.98 | 0.72 | 0.77 | 0.85 |
| pm-statement-to-the-house-of-commons… | 0.81 | 0.76 | 0.98 | 0.70 | 0.75 | 0.60 |
| president-zelenskyy-to-visit… | 0.97 | 0.97 | 0.95 | 0.92 | 0.88 | 0.95 |
| prime-minister-makes-offer-to-holyrood… | 1.00 | 1.00 | 0.83 | 1.00 | 0.91 | 0.91 |
| uk-government-response… | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 | 1.00 |
| **Mean** | **0.87** | **0.93** | **0.94** | **0.82** | **0.83** | **0.83** |

- **The larger models agree with themselves more than Haiku does.** Opus is the most self-consistent and Haiku the least.
- **Between any two models, agreement is about 0.82, a little below Haiku's agreement with itself (0.87).** So across the whole run, a different model changes placing only somewhat more than drawing Haiku again does. But because Sonnet and Opus are more self-consistent, their gap from Haiku, and from each other, is mostly model difference rather than noise: Sonnet–Opus is 0.83 against within-model scores of 0.93 and 0.94.
- **Three fixtures are the same on every model:** the Lord-Lieutenant, the Holyrood offer (bar one Opus sample) and the reaction. On them, which model runs placing makes no difference.
- **On *Great British Railways*, Haiku's samples differ from each other (0.43) as much as they differ from the other models (0.42–0.45).** That is sampling noise. Sonnet and Opus each settle on one grouping (0.82), and the two groupings are not the same (0.52).
- **On the Commons statement and the Zelenskyy meeting, the difference is between models.** Sonnet–Opus agreement (0.60, 0.77) is well below what either model scores against itself.

## Kinds of difference, and how often

Counted over Fact pairs: every Fact of every fixture, across every pair of samples (930 pairs within a model, 2,325 between two).

| Pair | Same | Development by one only | Different grouping of new Stories | New Story by one, candidate by the other | Different candidate Story |
|---|---|---|---|---|---|
| Haiku within | 83.4% | 4.9% | 10.5% | 1.1% | 0 |
| Sonnet within | 90.3% | 4.9% | 4.7% | 0 | 0 |
| Opus within | 94.6% | 2.6% | 2.8% | 0 | 0 |
| Haiku–Sonnet | 77.6% | 10.8% | 10.2% | 1.3% | 0 |
| Haiku–Opus | 79.3% | 14.4% | 5.7% | 0.6% | 0 |
| Sonnet–Opus | 78.5% | 11.0% | 9.7% | 0.9% | 0 |

By modal placing over the 93 Facts:

| Pair | Same | Development by one only | Different grouping | New Story against candidate |
|---|---|---|---|---|
| Haiku–Sonnet | 75 | 10 (5 Haiku only, 5 Sonnet only) | 7 | 1 |
| Haiku–Opus | 76 | 14 (12 Haiku only, 2 Opus only) | 3 | 0 |
| Sonnet–Opus | 74 | 10 (10 Sonnet only, 0 Opus only) | 8 | 1 |

What the differences are mostly of:

- **Whether a Fact becomes a Development at all.** Opus leaves out more than either smaller model. Of the Facts the two models settle differently on placing at all, Opus places 2 that Haiku doesn't, against Haiku's 12, and none that Sonnet doesn't, against Sonnet's 10. What Opus leaves out is mostly supporting detail, a repeat, or a future arrangement: fares and seats, a train fleet, the funding package an apprenticeship is part of, a minister being due to update the House, the UK being due to take up a presidency.
- **How a new Story is cut.** Whether two related Facts start one new Story or two. Haiku's cut wanders from sample to sample. Each larger model cuts one way consistently, but not the way the other one does.
- **Never which candidate Story.** In no sample does one model file a Fact in candidate S2 while another files it in S3. When the models disagree about candidates, it is whether a Fact goes into one or nowhere, or into a candidate or a new Story.
- **Titles.** New Stories with the same Facts get near-synonymous titles on every model. Opus's titles vary least between samples.

### A Fact made a Development by one model and not another

*president-zelenskyy-to-visit-downing-street*, F1 (as at 2026-10-13): "The Prime Minister is set to host President Volodymyr Zelenskyy at Downing Street on 2026-10-13." The candidate was S2 *UK support for Ukraine* (shares Russia; Ukraine; United Kingdom; one Development, new funding for Ukraine's fifth winter).

| Haiku | Sonnet | Opus |
|---|---|---|
| left out ×5 | S2 ×4, left out ×1 | S2 ×5 |

*pm-meeting-with-president-zelenskyy-of-ukraine-14-october-2026*, F5: "The leaders discussed how the UK could accelerate defensive support to Ukraine ahead of the winter." The candidate was the same S2, by then holding missile deliveries and a £45m package.

| Haiku | Sonnet | Opus |
|---|---|---|
| left out ×5 | S2 ×5 | left out ×5 |

*pm-speech-in-scotland-16-october-2026*, F8: "An EU-UK Summit has been arranged for 20 November." It restates S8 *UK-EU summit and relations*'s one Development ("The UK-EU summit is scheduled for 20 November in Brussels").

| Haiku | Sonnet | Opus |
|---|---|---|
| left out ×5 | S8 ×3, left out ×2 | left out ×5 |

*pm-speech-in-scotland*, F11 "The UK government intends to back Scottish shipbuilding" and F12 "…a billion pounds into research and innovation across the UK". There was no candidate on either.

| Haiku | Sonnet | Opus |
|---|---|---|
| new Stories ×5 (*UK support for Scottish shipbuilding*, *UK research and innovation funding*) | left out ×5 | left out ×5 |

*pm-statement-to-the-house-of-commons-12-october-2026*, F20: "The UK is due to take up the presidency of the G20 in 2027." F21: "The UK intends to place AI centre stage in its G20 presidency."

| Haiku | Sonnet | Opus |
|---|---|---|
| F20+F21 new *UK G20 presidency* ×5 | F20+F21 new ×4 | F21 alone new ×5; F20 left out ×5 |

### A new Story started by one model, a candidate chosen by another

*pm-speech-in-scotland*, F15: "The Scottish government has announced a long-term programme for local government reform." The candidate was S16 *UK Government and Scottish Government cooperation on devolution*.

| Haiku | Sonnet | Opus |
|---|---|---|
| new *Scottish local government reform* ×4, S16 ×1 | S16 ×5 | new *Scottish local government reform* ×4, left out ×1 |

*pm-speech-in-scotland*, F6: "The Scottish government has agreed to match the UK government's £10 million to create two defence technical excellence colleges." Haiku files it in S16 ×3 and starts *UK defence skills and technical education* ×2; Sonnet and Opus file it in S16 ×5.

### New Stories cut differently

*great-british-railways-takes-shape-as-hq-and-apprenticeship-scheme-confirmed* (no candidates). F1: the HQ site in Derby; F2: up to 600 rail apprentices; F3: fares frozen; F4: 76,000 extra seats; F5: new GBR trains built in Britain; F6: the site holds 450 staff; F7: £1bn fleet for the north at Alstom Derby; F9: the apprenticeship is part of a £2.5bn youth employment package.

| Haiku (one cut per sample) | Sonnet | Opus |
|---|---|---|
| five cuts in five samples: {F1,F6}+{F2,F9}+{F3}+{F4}+{F5,F7}; {F2,F6}+{F3,F4}+{F5,F7}+{F9}; {F1–F7} as one Story; {F2–F7} as one; {F1,F3–F7}+{F2,F9} | {F1,F5,F6,F7} *GBR headquarters and rolling stock* + {F2,F9} *Rail foundation apprenticeship and youth employment* ×4 | one Story *Establishment of Great British Railways* holding {F1,F2} ×2, {F1,F2,F6} ×2, {F1,F6} ×1; fares, seats and fleet left out ×5 |

*pm-statement-to-the-house-of-commons*, F7 "new funding to help Ukraine through its fifth winter" and F8 "a new sanctions package covers over 90% of Russia's oil producing capacity". Haiku and Opus start two Stories ×5 (*UK support for Ukraine*, *UK sanctions on Russia*); Sonnet starts one ×5 (*UK support for Ukraine and sanctions on Russia*). F10 (a partnership with Merz on hybrid threats) and F11 (openness to Germany joining GCAP): Opus puts them in one Story ×5, while Haiku and Sonnet split them in 2 or 3 samples of 5. F16 (incidents at RAF Fairford and Molesworth) and F17 (the Home Secretary is due to update the House): Haiku and Sonnet mostly put them together; Opus files F16 alone ×5 and leaves F17 out.

### New Story titles

| Facts | Haiku | Sonnet | Opus |
|---|---|---|---|
| Holyrood F2–F4 | 5 different titles, on *working relations and devolution* | 4 different, on *offer… on empowering regions* | 2, *UK–Scottish Government cooperation on empowering Scottish regions* ×4 |
| Tyrone F1–F2 | *Lord-Lieutenant of County Tyrone* ×5 | the same ×5 | *Lord-Lieutenancy of County Tyrone* ×5 |
| Commons F19 | *UK-EU summit* ×3, two variants | *UK-EU summit* ×5 | *UK-EU relations and summit* ×4 |

### Reactions

**No difference was found.** On the reaction fixture (the government's response to the US–Russia agreement, placed first, with no candidates), every model leaves every Fact out on every sample, the response included. The Facts elsewhere that report someone speaking (the ministers' statements on *Great British Railways*, F10–F13) are left out by every model too. On the existing fixtures, the examples of reaction are thin. A fixture where a reaction meets a candidate Story it could be filed in, such as the *Cost of Living update* that prompted this issue, would test it; it is drafted as a follow-up rather than added here.

## Tokens per call

These come from each call's usage as returned, which is what a call's page shows (input, output). Thinking is from the response's `output_tokens_details.thinking_tokens` and is part of the output. Every call was a single attempt; the input is the same request on all three models.

| Model | Calls | Input, mean (min–max) | Output, mean (min–max) | Thinking, mean (max) | Calls that thought |
|---|---|---|---|---|---|
| Haiku 5.5 | 40 | 3,396 (2,351–4,706) | 1,179 (518–2,554) | not kept, about 900 (estimate) | not kept |
| Sonnet 5.5 | 40 | 3,396 (2,351–4,706) | 322 (153–1,320) | 34 (648) | 3 of 40 |
| Opus 5.5 | 40 | 3,396 (2,351–4,706) | 542 (242–1,326) | 259 (662) | 39 of 40 |

Haiku's committed recordings keep only input and output, not the raw response, so its thinking can't be read from them. The estimate assumes an answer the size of the others' (about 285 tokens without thinking), which leaves about three quarters of Haiku's output as thinking. On these defaults Haiku thinks the most and Sonnet hardly at all.

Mean per call by fixture (input / output / thinking):

| Fixture | Haiku | Sonnet | Opus |
|---|---|---|---|
| appointment-of-lord-lieutenant… | 2,421 / 636 | 2,421 / 224 / 43 | 2,421 / 293 / 111 |
| great-british-railways… | 3,364 / 1,164 | 3,364 / 330 / 0 | 3,364 / 552 / 261 |
| pm-meeting-with-president-zelenskyy… | 3,582 / 666 | 3,582 / 155 / 0 | 3,582 / 353 / 203 |
| pm-speech-in-scotland… | 4,706 / 2,134 | 4,706 / 344 / 0 | 4,706 / 736 / 380 |
| pm-statement-to-the-house-of-commons… | 3,518 / 2,288 | 3,518 / 904 / 229 | 3,518 / 1,268 / 610 |
| president-zelenskyy-to-visit… | 3,926 / 1,286 | 3,926 / 272 / 0 | 3,926 / 516 / 242 |
| prime-minister-makes-offer-to-holyrood… | 3,303 / 651 | 3,303 / 187 / 0 | 3,303 / 366 / 176 |
| uk-government-response… | 2,351 / 604 | 2,351 / 162 / 0 | 2,351 / 250 / 88 |
