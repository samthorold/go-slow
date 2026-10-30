# go-slow

The language of a slow-news briefing: the material that arrives from news sources and the editorial work done on it.

## Language

### Material

**Source**:
An outlet or publisher the owner has chosen, which **Collection** checks for new **Items**: a wire, a government department, Parliament, a pollster.
_Avoid_: Feed (a feed is one way of reading a **Source**)

**Reference source**:
A store of background facts about the world, such as Wikidata, a poll archive or a statistical series, consulted for context rather than collected from.

**Collection**:
Checking the **Sources** on a timetable and bringing their new **Items** into the **Record**.
_Avoid_: Ingestion, scraping, crawling

**Record**:
Everything collected, with every **Judgement** made on it and every piece of **Feedback**.
_Avoid_: Archive, database

**Item**:
One piece of material as collection brought it in from a source: a report, a release, a poll, a comment piece. The IPTC's "news item".
_Avoid_: Article, piece, story (for a single item)

**Span**:
A stretch of an **Item**'s text, as written, known by where it starts and ends: an address, not a thing with an identity of its own, so two **Judgements** about the same words are about the same **Span**. Spans nest, from the whole **Item** down to a word: "the city" is a **Span** inside "This is fantastic news for the city". A **Span** is never reworded or rewritten; what is said of it is said by **Judgements** on it.
_Avoid_: Statement, quote, sentence, chunk

**Cut**:
A **Judgement** listing an **Item**'s **Spans** before anything else is judged of them: the **Spans** editorial work starts from. A **Judgement** may still be about a **Span** no **Cut** lists ("the city" inside one that does).
_Avoid_: Segmentation, split

**Rejected**:
A **Span** judged not useful for editorial work ("Andy Burnham said:"). It stays in its **Item**; rejections are counted per **Source**.
_Avoid_: Deleted

**Refers to**:
A **Span** *refers to* another **Span** of the same **Item** when it names the same thing in other words: "the city" refers to "Derby", "he" to "Andy Burnham".
_Avoid_: Resolves to, rewrite

**Hydration**:
Judging what a **Span**'s **Spans** refer to, from the whole of its **Item**. The hydrated reading of a **Span** ("fantastic news for Derby") is worked out by following *refers to*, never written.

**Speaker**:
Whose words a **Span** is: a **Span** of the same **Item** naming them ("Andy Burnham", "a government spokesperson"). A **Span** with no speaker judged is the publisher's own.
_Avoid_: Attribution, author, role

**Entity**:
A thing in the world that can be referred to again: a person, organisation, place, or named occasion (the Budget, the 2024 general election), with an identity of its own, whatever any **Item** says of it. Some come from Wikidata, others are made by the **Editor**.
_Avoid_: Event (something that happened is said by a **Span**)

**Mention**:
A **Span** *mentions* an **Entity** when a **Span** inside it names the **Entity**, by name or by office ("Burnham", "the Prime Minister"), or *refers to* one that does. A mention says only that the **Span** names the **Entity**, not what part the **Entity** plays in anything.
_Avoid_: Tag, reference

### Editorial work

**Judgement**:
Something go-slow decided about **Spans**, kept with its **Judge** and never changed: a **Cut**, a *refers to*, a **Speaker**, a *follows*, a gathered **Story**, a **Briefing** statement, an **Assessment**.
_Avoid_: Annotation, label, inference

**Judge**:
What made a **Judgement**: the act, the version of its instructions, and the model. A new version of an act is a new **Judge**.
_Avoid_: Annotator, model (a model is one part of a **Judge**)

**Follows**:
A **Span** *follows* an earlier-published **Span** of another **Item** when it moves on what that one said: the speech that argues what the press release said it "will argue". Many-to-many: a **Span** can follow several, and be followed by several.
_Avoid_: Updates, continues (a chain, which *follows* is not)

**Same as**:
Two **Spans** of different **Items** are the *same as* each other when they say the same thing. Said by two independent **Sources**, it is corroboration; said twice by one publisher, it is not.
_Avoid_: Duplicate

**Contradicts**:
Two **Spans** of different **Items** *contradict* each other when both cannot hold.

**Story**:
A **Span**'s story: the **Spans** it moves on from and those that move it on, as editorial work gathers them at a time. It is a view from its **Span**, not a thing **Spans** belong to, so it has no identity of its own and every **Span** has one.
_Avoid_: Running story, developing story, thread

**Timeline**:
A **Story**'s **Spans** in the order they were published.

**Assessment**:
A **Judgement** the **Editor** reaches by reasoning over **Spans**, citing the **Spans** and earlier **Assessments** it rests on, as at a date. Unlike a **Span** it is go-slow's own words.
_Avoid_: Analysis (the format of the **Sunday read**), finding

### Output

**Briefing**:
The daily output: what the **Editor** judges worth telling of the **Spans** read since the last one, in statements each citing the **Spans** it rests on.
_Avoid_: Newsletter, digest, summary

**Sunday read**:
The weekly output, alongside Sunday's **Briefing**: analysis that puts the week's **Stories** into wider context from the **Record** and **Reference sources**.
_Avoid_: Weekly briefing, recap, long read

### People

**Editor**:
The agent that does the editorial work: making the **Judgements** on what the **Record** holds, and writing the **Briefing** and the **Sunday read**.
_Avoid_: Assistant, pipeline

**Owner**:
The person who chooses the **Sources** and **Topics** and gives the **Editor** **Feedback**. Anyone else who reads reads the same output.
_Avoid_: User, reader (a reader only reads; the **Owner** also steers)

**Feedback**:
What the **Owner** tells the **Editor**, as at the time it is given. It is one of four kinds: *wrong* (a **Judgement** is mistaken), *unwanted* (a **Span** told shouldn't have been), *missed* (a **Span** should have been told) or *new to me* (something told showed the **Owner** what they hadn't seen). A piece the **Owner** takes back is *undone*: still kept, but no longer standing.
_Avoid_: Correction (*new to me* corrects nothing), rating; deleted or retracted (for undone)

### Interest

**Topic**:
A standing area of interest the owner follows. It has no starting event, unlike a **Story**. It may be about an **Entity** (Reform UK) or be more abstract (standards in public life). It selects nothing the **Owner** is shown.
_Avoid_: Interest, beat, subject; Depth (how closely a **Topic** is followed is no longer said)

## Flagged ambiguities

- **"Story"** in newsroom usage means both a single published piece and the ongoing subject it belongs to (Reuters uses both senses in one sentence). Here it means only a **Span**'s story, across **Items**; a single piece is an **Item**.

- **"Briefing"** at *The Economist* is a long analytical section; for the IPTC it is a summary of many items. Here it is the short daily output; the long weekly analysis is the **Sunday read**.

- **"Fact"** and **"Claim"** are not used: a **Span** is what a publisher wrote, whoever its **Speaker**, and corroboration is *same as* across independent **Sources**.

## Example dialogue

> **Dev:** The Holyrood speech came in this morning. Its line that the next stage of devolution should put more decisions with Scotland's regions: is that new?
> **Owner:** No, it *follows* Tuesday's press release, which said the PM "will argue" exactly that. The speech is the **Span** that moves it on.
> **Dev:** And "Scotland is one of the most centralised nations in Europe" is in both.
> **Owner:** That's *same as*, but it's one publisher saying it twice, so it isn't corroboration.
> **Dev:** And the GBR headquarters release says "this is fantastic news for the city". Whose words?
> **Owner:** Its **Speaker** is the Mayor, and "the city" *refers to* Derby, so the **Span** mentions Derby. The **Briefing** tells it as the Mayor's view, never in its own voice.
