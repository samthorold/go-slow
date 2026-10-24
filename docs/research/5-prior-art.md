# Prior art: what news tools do editorially

The question (issue 5): what do existing news tools do editorially, as opposed to only aggregating? Set against the editorial work in [`CONTEXT.md`](../../CONTEXT.md).

## Opinion

**go-slow's editorial work is mostly not done by existing tools, and the parts it shares with them are the plumbing.** Grouping coverage of one event, extracting entities, and summarising a set of articles are common. Following a **Story** as an ongoing subject, telling **Facts** from **Claims**, connecting by cause, and analysis that builds on earlier analysis are not. Those four are where go-slow has to earn its keep, and why the end state's *foresight* part is load-bearing rather than a bonus.

- **Stories.** Every tool that groups material groups it by a single event over a short window. The industry's "story" is our **Item** cluster around one event, not our **Story**. The only grouping that follows a subject over time, with an updating timeline, is in Feedly's threat-intelligence product (attack and actor cards), outside news for general readers. Our "story" means something the tools' "story" does not, which the flagged ambiguity in `CONTEXT.md` already covers.
- **Facts and Claims.** Most tools rate credibility per outlet, borrowed from third-party raters. Claim-level work exists only in NewsAPI.ai's sentence labels (Fact, Opinion, Forecast), Particle's tracing of each summary bullet to its source excerpts, and Feedly's separation of events that happened from possible ones. None separates "X says Y" from "Y is established", which is our **Claim** keeping its attribution.
- **Connections.** Tools connect subjects by shared entities or by similarity. None connects by cause; Event Registry named cause and effect between events as future work in 2014. Our **Connection** between **Facts** with a kind (cause) has no counterpart found.
- **Periodic analysis.** It is either a prompt run over a set of articles (Inoreader reports, Feedly newsletters) or a fixed daily format (Kagi, Ground News). None builds on the previous period's analysis, which is what the **Sunday read** does by drawing on the **Record**.
- **Personalisation** splits by philosophy: Feedly, Particle and ChatGPT Pulse learn from the reader; Kagi News and Google's Full Coverage are deliberately the same for everyone. go-slow sits with the second (the output is the same for every reader), steered by one **Owner**'s **Feedback** rather than learned per reader.
- **A warning.** Apple withdrew its AI news notification summaries in January 2025 after they misstated BBC headlines. Summaries that lose attribution fail publicly, which is the risk **Claims** keeping their attribution guards against.

Vocabulary worth noting but not borrowing: Event Registry's "story" (one language) versus "event" (across languages), and NewsAPI.ai's "facticity". Both name single-event or per-sentence ideas, not ours.

## Grounding

Tools covered: Inoreader, Feedly, Ground News, NewsAPI.ai (Event Registry), Particle, Kagi News, Google News Full Coverage and Discover, Perplexity Discover, ChatGPT Pulse, Artifact, and Apple Intelligence's withdrawn summaries.

Evidence quality:
- **Read in full:** the Event Registry paper (WWW 2014).
- **Read as pages through a fetch tool that paraphrases** (short quotes checked): Feedly's docs, Inoreader's blog, Ground News's FAQ and help, Particle's help and App Store listing, Kagi's blog and data-format docs, NewsAPI.ai's home page and event-types post, Google's News retrospective.
- **Snippets or press coverage only:** Perplexity Discover, ChatGPT Pulse, Artifact, Apple Intelligence, Google Discover's recent features, Feedly's threat-actor cards.
- **Not sourced:** current NewsAPI.ai docs (JavaScript-rendered), OpenAI's Pulse page (403), how Ground News, Particle and Kagi cluster stories.
- **Where sources corrected recall:** Ground News rates factuality per outlet, not per story; NewsAPI.ai's Fact/Opinion/Forecast labelling was unknown; Feedly clusters as well as deduplicates; Particle's political spectrum is from outside raters, not AI.

The opinion rests mainly on the tools' own documentation; a capability a tool has but doesn't document would be missed.
