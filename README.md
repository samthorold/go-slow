# go-slow

Slow news: a daily briefing, with a longer read on Sundays that puts the week into wider context.

The value is the editorial work: following how things unfold, linking across items, telling claims from facts against primary sources, and surfacing what a casual reader would miss. Collection from news sources is plumbing, done politely on its own timetable.

Early design. Go and htmx. The working concepts are in the issues.

## How it flows

Collection runs every few minutes. Reading, placing and connecting run in batches, each applied on a later Collection run, so the acts follow one another. The Briefing is written in real time once the morning's work is done. The terms are in [`CONTEXT.md`](CONTEXT.md); the reasons for this shape are in the [system design](docs/system-design/README.md).

```mermaid
flowchart TD
    subgraph Sources["Sources (fetched politely)"]
        GOV[GOV.UK: No 10, UK Space Agency]
        PARL[Parliament written statements]
        ONS[ONS releases]
    end

    Sources -->|every Collection run| COLLECT[Collection<br/>feed + full text]
    COLLECT --> ITEMS[(Items)]
    ITEMS --> TOPIC{Ties to a<br/>followed Topic?}
    TOPIC -->|no| IDLE[Kept, not read]
    TOPIC -->|yes| READ

    subgraph Batches["Daily acts: batches, each applied on a later Collection run"]
        READ[Reading<br/>Item → Facts, Claims, mentions]
        PLACE[Placing<br/>Facts → Developments in Stories]
        CONNECT[Connecting<br/>Roles + cause Connections]
        READ --> PLACE --> CONNECT
    end

    WD[(Wikidata)] -.->|resolves Entities| READ
    FB[(Owner's Feedback)] -.->|precedents| READ
    FB -.->|precedents| PLACE

    CONNECT --> RECORD[(Record: Stories, Timelines,<br/>Entities, Connections)]

    RECORD --> WAIT{Past BRIEFING_TIME<br/>and no work awaiting?<br/>waits at most 4h}
    WAIT --> RECON[Reconnect<br/>real time, the past day's Facts]
    RECON --> BRIEF[Write the Briefing<br/>real time]
    BRIEF -->|Sundays| SUNDAY[Sunday read<br/>agent with tools over the Record + Wikidata]

    BRIEF --> OWNER((Owner))
    SUNDAY --> OWNER
    OWNER -->|Wrong, Unwanted, Missed, New to me| FB
    FB -->|marks, never deletions| RECORD
```
