# System design

The design of go-slow: how it models the editorial work of a slow-news briefing, and why it has this shape. Terms are defined in [`CONTEXT.md`](../../CONTEXT.md). Hypotheses are stated here without status; status is in the register, derived from readings, and until the first reading every hypothesis is Due.

## End state

**The Owner keeps reading the Briefing and the Sunday read.** Continued use is the top because the mechanism of go-slow's value is hard to pin down: whatever it is, a reader who keeps coming back is getting it, and a reader who stops is not. go-slow sits on top of the outlets it collects from, not in place of them, so the test is use, not displacement of other news. The parts below are the faster, finer readings that say why use holds or falls.

*Reading rule.* Over a rolling 8-week window, count every day a **Briefing** is opened and every **Sunday read** opened.
- **Refuted**, in any window: fewer than 2 days in 7 on average, or 3 or fewer of the 8 **Sunday reads**. Either output being abandoned refutes it, since both are the end state.
- **Supported**, only in a quiet window (commits to go-slow on no more than 1 day in 7 on average): at least 4 days in 7, and at least 6 of the 8 **Sunday reads**.
- **Undecided**: anything else.

The **Owner** is also the builder, and opens the outputs to check work as well as to read them. That error inflates use, so a busy window can refute but never support: if even the builder doesn't open the **Briefing**, that is strong evidence; if the builder does, it is weak. Separating reading from checking by excluding days with commits fails while development is daily, because it excludes nearly every day. An undecided window is re-read on the next fresh window, judged alone. Review every 8 weeks once the **Briefing** runs daily.

### Necessary conditions

Three cheap, coarse conditions for any use at all. Each runs on every change and on the running system; any failure refutes, and passing confirms nothing. Without them a broken **Collection** or a missing **Briefing** would surface weeks later, as a dip in use.

- **The outputs appear.** A **Briefing** every day and a **Sunday read** every Sunday, by a time the **Owner** sets. Refuted by any missed day.
- **Everything traces.** Every **Development** in a **Briefing** traces to at least one **Item** in the **Record**, and every **Fact** to an **Item** or a **Reference source**. Refuted by any untraced one. Statements with nothing behind them are the way AI news summaries fail in public (see [prior art](../research/5-prior-art.md)), and this is the mechanical guard against it.
- **No Source goes quiet.** Every **Source** yields new **Items** within its usual gap. Refuted by any **Source** silent past it without notice.

### Read from Feedback

The four parts below are all read from one instrument, the **Feedback** in the **Record**, by its kind. The **Owner** gives **Feedback** in the course of reading and building anyway, so these readings cost nothing extra and carry the signal while the top can't be supported. They need no ground truth of what mattered: an outside record (a year-in-review page, an outlet's weekly roundup) is sparse, one outlet's judgement, or not licensed to keep, and the **Owner**'s **Topics** and **Depths** are the only standard of what matters here anyway. Each count is broken down by level (**Story**, **Development**, **Fact**, **Connection**), so a part that moves shows where.

*Shared reading rule for the three parts that should fall.* Count **Feedback** of the part's kind per **Briefing**, from the **Record** and never by hand, over the last 4 weeks against the 4 before. **Refuted** if the rate rose; **supported** if it fell by at least a quarter; **undecided** otherwise. A fall also results from the **Owner** giving less **Feedback** at all, so these are read only while the top is not refuted. Due every 4 weeks and on any change to the **Editor**.

### Coverage: the Editor misses less

*Missed* **Feedback** falls: the **Stories**, **Developments** and **Connections** the **Owner** has to add become fewer.

### Brevity: the Editor includes less that isn't wanted

*Unwanted* **Feedback** falls: the **Owner** cuts less. Brevity is what the **Owner** stops cutting, not a reading-time budget, because a budget would cut what matters on a heavy day and pad a light one.

### Accuracy: the Editor gets less wrong

*Wrong* **Feedback** falls: fewer **Facts** untrue as at their date, **Claims** taken for **Facts**, or mistaken **Connections**. Because the **Record** feeds the editorial work, accuracy is what keeps the other parts from degrading as the **Record** grows.

### Foresight: the Editor shows the Owner what they hadn't seen

*New to me* **Feedback** keeps coming, mostly on **Connections** and on the **Sunday read**. This is where go-slow differs from existing news tools, none of which follows a **Story** as an ongoing subject or connects subjects by cause (see [prior art](../research/5-prior-art.md)).

*Reading rule.* Over each 4-week window, **refuted** if there is no *new to me* **Feedback**; **supported** if there is some in at least 3 of the 4 **Sunday reads**; **undecided** otherwise. Read only while the top is not refuted. Due every 4 weeks.

## Shape

The editorial work is the one core domain. Collection and storage are adapters around it (the Web apps Guide's ports and adapters): they fetch and keep material, and make no editorial judgement. The value of the project is the editorial work, so it is the part kept free of I/O and tested directly.

**An agent edits, and the Owner steers.** The **Editor** does the editorial work every day without waiting on the **Owner**, guided by the **Owner**'s **Topics** and **Depths**. The **Owner** reads its output and gives **Feedback** on it, and the **Feedback** is kept in the **Record**. Steering is **Feedback** too: telling the **Editor** a **Story** was missed or unwanted is how the **Owner** influences which **Stories** they are shown. A briefing the **Owner** had to edit would wait on the **Owner**'s time, the opposite of one they read. An **Editor** with no **Feedback** loop would let its mistakes compound, because the **Record** feeds the editorial work: later **Connections** are made over earlier **Facts**, so a wrong **Fact** left in place spreads through every **Connection** built on it. What form the **Editor** takes is open.

**The editorial output is the same for every reader.** The owner's **Topics** and their **Depths** shape the editorial work, and everyone who reads the briefing reads the same one. There is no model of an audience: no per-reader selection, no segments, no engagement signal. A briefing personalised per reader would multiply the editorial work by the number of readers and turn judgement into ranking. One output keeps the editorial work the thing that is done, once.

## Stories

**Stories don't nest.** A wider story is a **Story** like any other, sharing **Developments** with narrower ones. Because one **Fact** can be a **Development** in several **Stories**, the ITV investigation's resignations sit in both *the ITV investigation into Reform* and *Reform's standing* without either containing the other. A hierarchy would break on the common case of a story belonging to two wider ones (that investigation is also part of *undercover journalism into parties*), so it would be a graph anyway, with a false tree implied. Building a wider story is starting a **Story** and gathering existing **Developments** into it, which is much of what the Sunday read does.

**Connections are made between Facts and derived between Stories.** The editorial work makes **Connections** from a **Fact** to another **Fact** (the investigation caused the resignations) and from a **Fact** to the **Entities** it involves (ITV Exposure investigated Reform UK). Two **Stories** are connected when they share **Developments**, or when their **Facts** involve the same **Entities** in the same roles: that is how "other investigations into Reform" and "who carried out the investigation" are found. A **Connection** stored directly between **Stories** would hold its reason only in the editor's head and go stale as the **Stories** grow. A derived one always points at the shared **Facts** that justify it.

## Facts and Claims

**A Fact is corroborated against the primary Item where the Record holds one.** The text of a speech, a statutory instrument or a poll's tables outranks reports of them, and an original report outranks a pickup of it. A proposition the **Editor** can't corroborate stays a **Claim**, attributed to whoever made it. Because **Collection** never searches, a primary document is in the **Record** only if its publisher is a **Source**. That is a reason to choose primary publishers (GOV.UK, Parliament, pollsters) as **Sources** alongside the outlets that report on them.

**A Fact holds as at a date.** What was true when it was recorded may not be true now (a "former" leader, a PM since a given day). History in the **Record** stays true of its date and is never rewritten to the present.

## How material arrives

**News material arrives only through collection.** Collection checks a fixed set of known sources on its own timetable, separate from the briefings', and lands what it finds in the record. The daily briefing and the Sunday read both work over that record; neither fetches news material of its own.

One way in keeps every request to a news source on one timetable, where politeness (request rates, conditional requests, robots.txt) is enforced in one place. A Sunday read that fetched topic-led material while it was being written would be a second, irregular visitor to the same outlets, with its own politeness to get right, and would turn collection's "check the same known sources" into topic search.

**The record keeps everything collected.** Every item collection brings in stays in the record, including the most that warrant nothing, and the editorial work is layered over it. Whether a detail matters depends on how closely the owner follows its topic, and that changes: something passed over today becomes worth linking to once the owner follows its topic closely. With no backfill, an item dropped when it warranted nothing could never be recovered. How much of each item is kept (full text, or only its headline, link and excerpt) depends on each outlet's terms of use, and is open.

**The owner chooses the sources; topics don't.** The known sources are a list the owner maintains by hand. Topics of interest act only in the editorial work, on what collection brings in. Following a new topic may lead the owner to add a source, but nothing derives sources from topics: doing so would need source discovery from a topic, which is topic search under another name, and would let the list grow beyond what can be visited politely.

**Reference sources are consulted, not collected.** Wikidata and historic datasets (past polls, election results) hold background facts about the world rather than news. The editorial work looks things up in them when it needs that background, for instance the Sunday read placing a week's poll against months of earlier ones.

**The record starts empty and is never backfilled.** Collection's first run sees only what the known sources carry that day, and the record deepens week by week. Early Sunday reads therefore have the long-run facts, from reference sources, but no earlier news: an investigation from last year is not in the record until something brings it back. Backfilling from the sources' archives would close that gap, but it is a heavy one-off visit to every outlet, the kind of fetch politeness rules out, and it would leave months of material with no editorial work done on it. The gap closes on its own as the record grows.
