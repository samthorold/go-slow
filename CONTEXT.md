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

**Item**:
One piece of material as collection brought it in from a source: a report, a release, a poll, a comment piece. The IPTC's "news item".
_Avoid_: Article, piece, story (for a single item)

**Span**:
A stretch of text, known by where it starts and ends: an address, not a thing with an identity of its own, so two **Judgements** about the same words are about the same **Span**. Most are of an **Item**'s text, as written; the rest are **Editor's Spans**. Spans nest, from the whole **Item** down to a word: "the city" is a **Span** inside "This is fantastic news for the city". A **Span** is never reworded or rewritten; what is said of it is said by **Judgements** on it.
_Avoid_: Statement, quote, sentence, chunk

**Editor's Span**:
A **Span** the **Editor** wrote, in the **Judgement** that makes a **Story**, citing the **Spans** it rests on. Its **Speaker** is always the **Editor**. It may say more than what it cites can support, and does not reword it.
_Avoid_: Assessment, statement, summary

**Entity**:
A thing in the world that can be referred to again: a person, organisation, place, or named occasion (the Budget, the 2024 general election), with an identity of its own, whatever any **Item** says of it. Some come from Wikidata, others are made by the **Editor**.
_Avoid_: Event (something that happened in the world is said by a **Span**; an **Event** is something kept in the **Record**)

### The Record

**Record**:
Every **Event**, in the order it happened.
_Avoid_: Archive, database

**Event**:
Something that happened, kept in the **Record** and never changed: an **Item** collected, its **Cut**, or a **Judgement**.
_Avoid_: Log entry, update

**Command**:
A step of the **Editor**'s work: it reads a **Projection** and outputs zero or more **Judgements**. The only thing that writes a **Judgement**.
_Avoid_: Act, pass, job

**Projection**:
What the **Events** say about something as at a time, worked out by plain code that only reads: the **Stories** a **Span** is in, a **Command**'s input, a **Page**.
_Avoid_: View, read model, state

**Page**:
A **Projection** the **Owner** opens. Opening one changes nothing.
_Avoid_: View, screen

### Judgements

**Judgement**:
An **Event** a **Command** outputs about **Spans** or **Stories**, kept with its **Judge**: a *refers to*, a **Speaker**, a **Rejected**, a name and the **Entity** it matches, a new **Story**, a **Name**.
_Avoid_: Annotation, label, inference

**Judge**:
What made a **Judgement**: the **Command**, the version of its instructions, and the model. A new version of a **Command** is a new **Judge**.
_Avoid_: Annotator, model (a model is one part of a **Judge**)

**Candidate**:
A **Judge** whose **Judgements** are kept beside the live ones and read by no **Projection** until the **Owner** chooses it; once chosen it is a live **Judge**.
_Avoid_: Shadow judge, experiment

**Cut**:
The **Event** at which an **Item** enters editorial work, once: plain code lists its **Spans** before anything else is judged of them, and puts them in one **Story**. It is not a **Judgement** and has no **Judge**. A **Judgement** may still be about a **Span** no **Cut** lists ("the city" inside one that does).
_Avoid_: Segmentation, split

**Hydration**:
Judging what a **Span**'s **Spans** refer to, what it names, who speaks it and whether it is **Rejected**, from the whole of its **Item**. The hydrated reading of a **Span** ("fantastic news for Derby") is worked out by following *refers to*, never written.

**Refers to**:
A **Span** *refers to* another **Span** of the same **Item** when it names the same thing in other words: "the city" refers to "Derby", "he" to "Andy Burnham".
_Avoid_: Resolves to, rewrite

**Speaker**:
Whose words a **Span** is: a **Span** of the same **Item** naming them ("Andy Burnham", "a government spokesperson"). A **Span** of an **Item** with no speaker judged is the publisher's own; an **Editor's Span** is the **Editor**'s.
_Avoid_: Attribution, author, role

**Rejected**:
A **Span** judged not useful for editorial work ("Andy Burnham said:"). It stays in its **Item** and in the **Story** its **Cut** made; rejections are counted per **Source**.
_Avoid_: Deleted

**Mention**:
A **Span** *mentions* an **Entity** when a **Span** inside it names the **Entity**, by name or by office ("Burnham", "the Prime Minister"), or *refers to* one that does. A mention says only that the **Span** names the **Entity**, not what part the **Entity** plays in anything.
_Avoid_: Tag, reference

### Stories

**Story**:
A group of **Spans** kept in the **Record**, as at one point in time: one **Event** makes it and nothing changes it. A **Cut** makes one from an **Item**'s **Spans**. The **Editor** makes the rest, each holding the **Editor's Spans** it wrote and the **Spans** they cite. A **Span** of an **Item** is in the one its **Cut** made, and in any number of the **Editor**'s, or none.
_Avoid_: Running story, developing story, thread, cluster, Item's Story, Editorial Story, Merge, Split, Listing

**Name**:
A short subject for a **Story** the **Editor** made, written for the **Owner** to recognise it by. No **Command** reads it.
_Avoid_: Title, headline, summary

**Timeline**:
A **Story**'s **Spans** in the order they were published.

### People

**Editor**:
What does the editorial work: the **Commands** that make the **Judgements**.
_Avoid_: Assistant, pipeline

**Owner**:
The person who chooses the **Sources** and **Topics** and reads the **Stories**. Anyone else who reads reads the same **Pages**.
_Avoid_: User, reader (a reader only reads; the **Owner** also chooses)

### Interest

**Topic**:
A standing area of interest the owner follows, given by its terms. Unlike a **Story** it groups no **Spans**: its **Page** lists the **Stories** that touch it, and that is how the **Owner** follows a subject. It may be about an **Entity** (Reform UK) or be more abstract (standards in public life). It gates none of the editorial work.
_Avoid_: Interest, beat, subject; Depth (how closely a **Topic** is followed is no longer said)

## Flagged ambiguities

- **"Story"** in newsroom usage means both a single published piece and the ongoing subject it belongs to (Reuters uses both senses in one sentence). Here it means only a group of **Spans**; a single published piece is an **Item**, and an ongoing subject is followed through a **Topic**.

- **"Event"** means something kept in the **Record**, never something that happened in the world: what happened in the world is said by a **Span**, and a named occasion (the Budget) is an **Entity**.

- **"Briefing"** names what go-slow is (a slow-news briefing), not a thing in it: the **Owner** reads **Stories**.

- **"Fact"** and **"Claim"** are not used: a **Span** is what a publisher wrote, whoever its **Speaker**. Whether two **Spans** say the same thing, contradict each other, or one moves the other on is not judged; that they belong together is a **Story**.

## Example dialogue

> **Dev:** The Holyrood speech came in this morning. Its line that the next stage of devolution should put more decisions with Scotland's regions: which **Story** is it in?
> **Owner:** The one its **Cut** made. Tuesday's press release said the PM "will argue" exactly that, but no story lies behind that link, so the **Editor** needn't make one.
> **Dev:** And if that line bears on a longer cause no **Source** connects it to?
> **Owner:** Then the **Editor** makes a new **Story**: its own words, citing that sentence and whatever else they rest on. Nothing moves: the sentence is still in the **Story** the speech's **Cut** made.
> **Dev:** And if two **Stories** turn out to be about the same thing?
> **Owner:** Neither changes, and neither does harm. A later run may make another that follows them.
> **Dev:** How do I follow devolution, then?
> **Owner:** It's a **Topic**. Its **Page** lists the **Stories** that touch it, newest first.
> **Dev:** And the GBR headquarters release says "this is fantastic news for the city". Whose words?
> **Owner:** Its **Speaker** is the Mayor, and "the city" *refers to* Derby, so the **Span** mentions Derby. That's **Hydration**, from its **Item** alone; no **Story** comes into it.
