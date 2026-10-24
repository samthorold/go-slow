# Sources: politeness, terms and licensing

The question (issue 2): how should go-slow visit a fixed list of sources politely and lawfully, and how much of each item may it keep? Widened during the grill to: how feed readers and AI news tools get their rights, which economics and market data sources are open, and which reporting outlets permit a private tool to store and summarise.

Not legal advice. Where the law or the terms are unclear, it says so.

## Opinion

**go-slow is primary-led and private, because nothing else is open to it.** The design's consequences are in [`../system-design/README.md`](../system-design/README.md) (*Sources are primary-led*, *go-slow is private*, *Collection is polite by these rules*, *Releases are collected; series are consulted*). The findings that force them:

- **The major UK outlets forbid storing and summarising.** The BBC's terms let a feed be displayed unchanged and credited, forbid "plucking" metadata from its feeds, and its robots.txt bans "summaries for your own use", AI grounding and agentic AI, naming about 40 AI agents. The Guardian limits use to personal and non-commercial, bans automated reading and any AI use; its free API requires deleting content within 24 hours. Reuters has had no public feed since 2020 and requires written consent for any automated collection.
- **Being private does not make storing lawful.** UK law permits the temporary copies made while viewing (CDPA s.28A; *PRCA v NLA* [Meltwater], UK Supreme Court 2013 and CJEU 2014), but stored copies need a licence, and there has been no private-copying exception since 2015. The text-and-data-mining exception (s.29A) covers non-commercial research only, and whether a personal tool is "research" is unclear. The government's March 2026 report dropped a commercial exception with opt-out. Headlines and short extracts can be protected works.
- **How others do it.** Feed readers hold no publisher licences: they display feeds unchanged with a link out and put responsibility on the user. AI news tools either hold deal-by-deal licences (OpenAI, Perplexity, Google, Microsoft, Particle, Apple News) or are being sued (NYT v OpenAI, Dow Jones v Perplexity). Kagi News uses public RSS without licences. The NLA licenses organisations, not a private individual's tool.
- **What is open is official material and analysis, not breaking news.**

| Kind of source | Admissible | What the Record may keep | Licence or terms |
|---|---|---|---|
| GOV.UK, government departments, devolved governments | Yes: Atom feeds, Content API (10 req/s) | Full text, with attribution | Open Government Licence v3.0; excludes personal data, logos, third-party material |
| UK Parliament, including Commons Library briefings | Yes: Developer Hub APIs; the main site and Hansard host block scripted clients | Full text, with attribution | Open Parliament Licence v3.0; same exclusions |
| ONS, OBR, HM Treasury, HMRC, DMO, Scottish statistics | Yes: ONS API (about 200 req/min, bot User-Agent with contact); release calendars as iCal, RSS, JSON | Full data and release text | OGL. DMO's site blocks bots; gilt reference prices are Tradeweb's, free non-commercially from noon the next day |
| Bank of England | Statistics yes (but robots.txt disallows the CSV endpoint); MPC text personal use only | Series as reference; MPC text unclear | OGL for statistics except some third-party FX; BoE copyright for minutes, reports, speeches |
| ECB, Eurostat, World Bank, OECD, BIS, BLS, BEA, US Treasury | Yes, via APIs | Full data | Free reuse with citation, CC BY 4.0 or public domain. OECD is slow (about 60 downloads/hour) |
| FRED, IMF | No | Nothing | FRED bans storing and any LLM use; IMF bans automated bulk download without permission |
| Commercial market data (Yahoo, FTSE Russell, S&P, ICE) | No | Nothing | Licensed. EODHD alone explicitly permits private storage. Open alternatives: official yield curves, ECB FX rates, EIA prices, BIS policy rates |
| Pollsters | Ipsos yes; YouGov no | Ipsos: results with attribution; the British Polling Council's disclosure fields | Ipsos treats published polls as public domain; YouGov bans bots |
| Open reporting and analysis | LSE British Politics and Policy, Economics Observatory, Wikipedia | Full text, with attribution | CC BY 4.0, CC BY-SA 4.0 |
| Open with a catch | openDemocracy, Carbon Brief | Full text, summaries kept private | CC BY-NC 4.0, CC BY-NC-ND 4.0: 4.0 lets adaptations be made if not shared |
| Restricted reporting | BBC, Guardian, Reuters, Full Fact, the Local Democracy Reporting Service; The Conversation (robots.txt blocks AI agents) | Nothing | As above |
| No licence found | Institute for Government, Constitution Unit, UK in a Changing Europe, IFS, NIESR, The Ferret, Nation.Cymru | Nothing (treat as all rights reserved) | |

Mechanics found along the way: GOV.UK honours conditional requests (a live request returned 304); BBC feeds send no ETag (`max-age=3`, `ttl` 15). AI-use signals such as IETF `Content-Usage` and TDMRep are drafts or EU-only. Personal data in stored items (names in Hansard and releases) falls outside the open licences and makes the operator a controller under UK GDPR; this was recalled, not researched, and is open in the design.

## Grounding

Four research passes, each briefed from its question: polite collection and terms of use; how feed readers and AI tools license content; economics and market data sources; openly licensed reporting outlets.

Evidence quality:
- **Read in full:** the BBC's terms of use (v1.8) and robots.txt; the Guardian's terms of service and Open Platform terms; Reuters', GOV.UK's, YouGov's and Ipsos's robots.txt; the OGL v3.0 text; CDPA s.29A; the Supreme Court judgment in *PRCA v NLA*; the government's March 2026 Report on Copyright and AI (first 100,000 characters); the Creative Commons 4.0 licence texts; live feed headers for the BBC, GOV.UK and the Guardian.
- **Snippets or press coverage only:** publisher deal terms and lawsuit status; the Leicester Gazette and LDRS arrangements; the Open Parliament Licence wording; some API rate limits.
- **Not sourced:** ITV's and PA Media's terms (unreachable); the RSL specification.
- **Where sources corrected recall:** the UK has no private-copying exception (quashed 2015); the BBC forbids taking even headlines and links from its feeds; YouGov bans bots despite its CC BY-NC licence; FRED now bans storage and LLM use; CC NoDerivatives 4.0 permits private summaries; Full Fact is not openly licensed; a 403 on robots.txt permits access under RFC 9309; `Crawl-delay` is not in the RFC.
- **Method:** WebFetch was blocked for the BBC, Guardian and Reuters, so those pages were fetched with curl or Python using an honest User-Agent.
