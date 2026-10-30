# go-slow

Slow news: a daily briefing, with a longer read on Sundays that puts the week into wider context.

The value is the editorial work: following how things unfold, linking across items, telling claims from facts against primary sources, and surfacing what a casual reader would miss. Collection from news sources is plumbing, done politely on its own timetable.

Early design. Go and htmx. The working concepts are in the issues.

## How it flows

Collection runs every few minutes, cutting each new Item into Spans. Reading and relating run in batches, each applied on a later Collection run, so the acts follow one another; between them, what the readings name is looked up in Wikidata. The Briefing is written in real time at its set time, over what the Record holds then. The terms are in [`CONTEXT.md`](CONTEXT.md); the reasons for this shape are in the [system design](docs/system-design/README.md).

```mermaid
flowchart TD
    subgraph Sources["Sources (fetched politely)"]
        GOV[GOV.UK: No 10, UK Space Agency, Treasury]
        PARL[Parliament written statements]
        ONS[ONS releases]
    end

    Sources -->|every Collection run| COLLECT[Collection<br/>feed + full text]
    COLLECT --> ITEMS[(Items)]
    ITEMS --> CUT[Cut<br/>Item → Spans]
    CUT --> READ

    subgraph Batches["Daily acts: batches, each applied on a later Collection run"]
        READ[Reading<br/>Spans: refers to, names, speakers, Rejected]
        LOOKUP[Look-ups<br/>names → Entities, plain code]
        RELATE[Relating<br/>Span follows, same as, contradicts Spans read before]
        READ --> LOOKUP --> RELATE
    end

    WD[(Wikidata)] -.->|matches names| LOOKUP
    FB[(Owner's Feedback)]

    RELATE --> RECORD[(Record: events on Spans,<br/>and the Stories built before)]

    RECORD --> BRIEF[Write the Briefing<br/>real time, at BRIEFING_TIME]
    BRIEF -->|Sundays| SUNDAY[Sunday read<br/>agent with tools over the Record + Wikidata]

    BRIEF --> OWNER((Owner))
    SUNDAY --> OWNER
    OWNER -->|Wrong, Unwanted, Missed, New to me| FB
    FB -->|marks, never deletions| RECORD
```
