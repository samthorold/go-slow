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
Everything collected, with the editorial work done on it.
_Avoid_: Archive, database

**Item**:
One piece of material as collection brought it in from a source: a report, a release, a poll, a comment piece. The IPTC's "news item".
_Avoid_: Article, piece, story (for a single item)

**Story**:
An ongoing subject followed across days, made of the **Items** about it. An **Item** belongs to any number of **Stories**, including none.
_Avoid_: Running story, developing story, thread

**Entity**:
A thing in the world that can be referred to again: a person, organisation, place, or named occasion (the Budget, the 2024 general election), with an identity of its own, whatever any **Item** says of it. Some come from Wikidata, others are made by the editorial work.
_Avoid_: Event (something that happened is a **Fact**)

**Mention**:
An **Item** *mentions* an **Entity** when it names it, by name or by office ("Burnham", "the Prime Minister"). A mention says only that the **Item** refers to the **Entity**, not what part the **Entity** plays in anything.
_Avoid_: Tag, reference

**Involves**:
A **Fact** or **Claim** *involves* an **Entity** when the **Entity** has a **Role** in it. A name that only says who or where something else is ("Zelenskyy of Ukraine", "the GBR headquarters site") is not involved, and nor is who made a **Claim** unless it gives them a **Role**. An **Entity** a **Fact** involves is one its **Item** mentions.
_Avoid_: Connection (a **Connection** ties a **Fact** to another **Fact** or to an **Assessment**, never to an **Entity**)

**Role**:
The part an **Entity** plays in a **Fact** it involves, one of four: *actor* (who or what did it), *undergoer* (what or whom it was done to, or what the **Fact** says something of: the site, in "the Becketwell site is brownfield"), *place* (where) and *occasion* (the named occasion it happened at or belongs to). An act the **Fact** states as mutual (they met, spoke, agreed together) has each party as an *actor*; an act with a direction (urged, assured, welcomed, offered) has its doer as *actor* and the one it was directed at as *undergoer*. A collective is one *actor* only where the **Item** names it as one (the G7, the signatories of a joint declaration); two **Entities** are never made into a joint actor.
_Avoid_: Affected, target, subject (an undergoer need not be harmed, opposed or the topic of the **Item**)

### Editorial work

**Fact**:
A proposition the editorial work holds true as at a date, drawn from **Items** and **Reference sources** and corroborated. It carries no author's voice.

**Claim**:
A proposition attributed to someone and not corroborated. It keeps its attribution.
_Avoid_: Allegation, statement

**Development**:
A **Fact** the editorial work has judged moves a **Story** on. The same **Fact** can be a **Development** in several **Stories**.
_Avoid_: Update (on the wire, an update is a revised **Item**)

**Assessment**:
A judgement the **Editor** reaches by reasoning over **Facts**, citing the **Facts** it rests on, as at a date. Unlike a **Fact** it is not corroborated as a single proposition, and unlike a **Claim** it is go-slow's own.
_Avoid_: Analysis (the format of the **Sunday read**), finding, judgement

**Timeline**:
A **Story**'s **Developments** in order.

**Connection**:
An editorial tie between a **Fact** and another **Fact** (such as cause) or an **Assessment** (supporting or undercutting it). **Stories** are connected through the **Facts** they share and the **Entities** their **Facts** involve in the same roles.
_Avoid_: Link, relation

### Output

**Briefing**:
The daily output: the day's **Developments** in the **Stories** the **Owner**'s **Topics** and **Depths** select, with their **Connections**.
_Avoid_: Newsletter, digest, summary

**Sunday read**:
The weekly output, alongside Sunday's **Briefing**: analysis that puts the week's **Stories** into wider context from the **Record** and **Reference sources**.
_Avoid_: Weekly briefing, recap, long read

### People

**Editor**:
The agent that does the editorial work: drawing **Facts** and **Claims** from **Items**, judging **Developments** and making **Connections**.
_Avoid_: Assistant, pipeline

**Owner**:
The person who sets the **Topics** and their **Depths** and gives the **Editor** **Feedback**. Anyone else who reads reads the same output.
_Avoid_: User, reader (a reader only reads; the **Owner** also steers)

**Feedback**:
What the **Owner** tells the **Editor** about a **Story**, **Development**, **Fact**, **Connection** or **Assessment**. It is one of four kinds: *missed* (it should be there), *unwanted* (it shouldn't), *wrong* (it is mistaken) or *new to me* (it showed the **Owner** something they hadn't seen). A piece the **Owner** takes back is *undone*: still kept, but no longer standing.
_Avoid_: Correction (*new to me* corrects nothing), rating; deleted or retracted (for undone)

### Interest

**Topic**:
A standing area of interest the owner follows, at some **Depth**. It has no starting event, unlike a **Story**. It may be about an **Entity** (Reform UK) or be more abstract (standards in public life).
_Avoid_: Interest, beat, subject

**Depth**:
How closely the owner follows a **Topic**, which decides how much detail on it matters.

## Flagged ambiguities

- **"Story"** in newsroom usage means both a single published piece and the ongoing subject it belongs to (Reuters uses both senses in one sentence). Here it means only the ongoing subject; a single piece is an **Item**.

- **"Briefing"** at *The Economist* is a long analytical section; for the IPTC it is a summary of many items. Here it is the short daily output; the long weekly analysis is the **Sunday read**.

## Example dialogue

> **Dev:** PA's report on the Solihull councillors resigning came in this morning. Is that a new **Story**?
> **Owner:** No, it's an **Item** in the investigation into Reform. The **Story** has been running since the 14th.
> **Dev:** So the resignations go on its **Timeline**?
> **Owner:** Yes, as a **Development**, connected to the broadcast as its cause. Farage's reaction is in the same **Item**. That he said it is a **Fact**, but it doesn't move the **Story** on, so it isn't a **Development**.
> **Dev:** And the Ipsos poll?
> **Owner:** A **Fact**, with fieldwork before the broadcast. It's a **Development** in *Reform's standing*, not in the investigation. The two **Stories** are connected because both involve Reform UK, and the **Sunday read** can draw on that.
