# go-slow

Slow news: it acts like the Economist, seeing the stories behind the stories and taking a zoomed-out view of the long-term causes.

The value is the editorial work: writing what no single source says, across sources and over time, and citing what it rests on. Collection from news sources is plumbing, done politely on its own timetable.

Go and htmx. This describes the design; the code still builds an earlier one until epic #294's slices land.

## How it flows

Collection brings in each new Item, and the Cut splits it into Spans and puts them in one Story. Hydrating and looking up names work on those Spans. Weaving reads Stories and writes new ones, in the Editor's own words, citing what they rest on. A Story never changes, and the Owner reads them newest first, or through a Topic. The terms are in [`CONTEXT.md`](CONTEXT.md); the reasons for this shape are in the [system design](docs/system-design/README.md).

```mermaid
flowchart TD
    subgraph Sources["Sources (fetched politely)"]
        GOV[GOV.UK: No 10, UK Space Agency, Treasury]
        PARL[Parliament written statements]
        ONS[ONS releases]
    end

    Sources -->|every Collection run| COLLECT[Collection<br/>feed + full text]
    COLLECT --> ITEMS[(Items)]
    ITEMS --> CUT[Cut<br/>Item → Spans, in one Story]
    CUT --> RECORD[(Record: Events)]

    subgraph Commands["Commands: read the Record, add Events, each in its own time"]
        HYDRATE[Hydrating<br/>Span: refers to, names, speaker, Rejected]
        LOOKUP[Looking up a name<br/>name → Entity, plain code]
        WEAVE[Weaving<br/>Stories → new Stories: the Editor's Spans and what they cite]
        NAME[Naming<br/>Story → Name, for the Owner]
    end

    RECORD <--> HYDRATE
    RECORD <--> LOOKUP
    RECORD <--> WEAVE
    RECORD <--> NAME
    WD[(Wikidata)] -.->|matches names| LOOKUP

    RECORD --> PAGES[Pages<br/>Stories, Topics, each Story and Span]
    PAGES --> OWNER((Owner))
```
