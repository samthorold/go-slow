# System design

The design of go-slow: how it models the editorial work of a slow-news briefing, and why it has this shape. Terms are defined in [`CONTEXT.md`](../../CONTEXT.md); where a term here is not yet there, it is provisional.

## Shape

The editorial work is the one core domain. Collection and storage are adapters around it (the Web apps Guide's ports and adapters): they fetch and keep material, and make no editorial judgement. The value of the project is the editorial work, so it is the part kept free of I/O and tested directly.

**An agent edits, and the Owner steers.** The **Editor** does the editorial work every day without waiting on the **Owner**, guided by the **Owner**'s **Topics** and **Depths**. The **Owner** reads its output and corrects it, and a correction is kept in the **Record**. A briefing the **Owner** had to edit would wait on the **Owner**'s time, the opposite of one they read. An **Editor** with no correction loop would let its mistakes compound, because the **Record** feeds the editorial work: later **Connections** are made over earlier **Facts**, so a wrong **Fact** left in place spreads through every **Connection** built on it. What form the **Editor** takes is open.

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
