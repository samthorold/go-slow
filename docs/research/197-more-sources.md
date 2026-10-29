# More Sources: space, economics and markets, long-form research

The question (issue 197): which further Sources could go-slow collect, varying by publisher and by kind of record, for three of the Owner's interests: space, economics and markets, and long-form research (academic papers, firms' and think tanks' research), arXiv and AQR by name? For each: what it publishes, its terms on automated collection and on keeping full text for a model to read privately, and how it can be read programmatically. A second pass after [`2-sources.md`](2-sources.md), which found the four Sources go-slow has and the economics bodies' terms; where the two overlap they agree.

Not legal advice. Where the terms are unclear or were not read, it says so.

## Opinion

**The terms sort candidates by who publishes, not by how.** Government and intergovernmental bodies under public-domain or open licences pass; private publishers (think tanks, investment firms, commercial outlets) mostly reserve their rights or bar robots by name. Growth for go-slow is in public bodies and openly licensed publishers.

**No publisher's terms address a model reading their text for one private reader.** "Personal use" clauses were written for a person printing a page, "derivative work" and "modification" for republication, "no robots" for crawlers. The only mentions of AI found were NASA's clause on attribution and its insignia, and the IMF's ban on training models. Outside open licences, a model's reading is *unaddressed*, not permitted. Whether it is "personal use" or a "derivative" is a judgement the terms don't make, so it belongs in go-slow's admission rule, written once, not argued Source by Source.

**Most candidates outside GOV.UK need a text fetch the design doesn't yet allow.** Whether their feeds carry full text was checked for none of them. Where an item carries only a summary, its text is on a page or in a PDF, and Collection reads feeds and APIs, never pages (system design: *Collection is polite by these rules*). For a publisher whose terms allow keeping the text, fetching its page honestly is no worse than an API; that is a design question, not settled here.

**arXiv is a metadata-and-abstract Source; AQR is out without permission.** arXiv's metadata is CC0 and its terms allow "your own personal use", but most papers carry arXiv's default licence, which "limits re-use of any type"; each paper's licence is in its metadata, so the gate an admission rule reads is per Item, not per Source. AQR forbids copying "any information obtained from this Website" without written consent, with no personal-use exception: asking is the only route.

| Candidate | Interest | Kind of record | Verdict | How it is read | Evidence |
|---|---|---|---|---|---|
| UK Space Agency (GOV.UK) | Space | News, guidance, research | Permitted (OGL v3.0) | GOV.UK Atom and Content API, as existing Sources; feed live and posting weekly (fetched 2026-10-29) | read (feed); OGL from `2-sources.md` |
| HM Treasury (GOV.UK) | Econ | News, speeches, statistics, consultations | Permitted (OGL v3.0) | As above; feed live (fetched 2026-10-29) | read (feed); OGL from `2-sources.md` |
| NASA | Space | News releases | Permitted (US public domain; acknowledge NASA; logos, third-party material excluded) | RSS [recall] | read (terms); recall (feeds) |
| GCAT (J. McDowell) | Space | Catalogue of objects, launches, events | Permitted (CC BY 4.0, cite) | TSV and text files, tar.gz | read |
| ESA | Space | News releases | Restricted (personal non-commercial; no derivatives or redistribution; silent on automated access) | 34 RSS feeds, free | read |
| JAXA | Space | News releases | Restricted (private use; no modification) | None found | read |
| Launch Library 2 | Space | Scheduled launches and events | Unclear (no licence stated) | API, 15 requests an hour keyless | read |
| Spaceflight News API | Space | Index of others' articles | Unclear (copyright line only) | API, docs unreadable | read |
| CelesTrak | Space | Orbital elements | Unclear (usage policy unread) | HTTP, once per two-hour update | read |
| Space-Track.org | Space | Orbital elements | Restricted (approved account; no transfer) | API, under 30 a minute | read |
| NOAA SWPC | Space | Space-weather alerts, data | Unclear (public domain likely [recall]) | Email, JSON [recall] | read (silent on terms) |
| FAA, Federal Register, UK CAA | Space | Regulatory | Unclear | Not established | search, recall, fetch failed |
| SpaceNews | Space | Independent outlet | Unclear (terms page answered 429) | Unknown | none |
| Federal Reserve Board | Econ | Releases, speeches, papers, data releases | Permitted (public domain, cite) | Keyless RSS, many feeds | read |
| ECB | Econ | Releases, speeches, papers, statistics | Permitted with attribution; authored papers restricted | Keyless RSS | read |
| Bank of England | Econ | Releases, speeches, publications | Restricted (personal use; statistics OGL) | Keyless RSS, eight feeds | read |
| OBR | Econ | Forecasts | Permitted (OGL) | Downloads only, no feed | read |
| Eurostat | Econ | Statistics | Permitted (reuse with acknowledgement) | SDMX and Statistics APIs | search |
| OECD | Econ | Publications, data | Permitted (CC BY 4.0 from July 2024) | Not looked up | search (terms page 403) |
| BIS | Econ | Publications, statistics | Restricted (non-commercial; silent on automation) | Not looked up | read |
| World Bank | Econ | Publications, data | Restricted (non-commercial; no derivatives) | APIs, "reasonable request volume" | read |
| IMF | Econ | Publications; statistical data | Forbidden for collection; data permitted with attribution | Not looked up | read |
| FCA | Econ | News | Forbidden (no bots; no copying into "any public or private electronic retrieval system") | RSS | read |
| SEC EDGAR | Econ | Filings | Permitted (public domain [recall]) | JSON APIs, RSS; 10 requests a second, declared User-Agent | read |
| Companies House | Econ | Filings | Unclear (its own sources contradict on licence) | API with key, 600 per 5 minutes | read (access); search (licence) |
| FCA NSM | Econ | Regulated announcements | Unclear (acceptable use policy unread) | None found | search |
| Resolution Foundation | Econ | Think-tank reports | Permitted (CC BY-NC-ND 4.0) | Feed unverified | read |
| Bruegel | Econ | Think-tank publications, blog | Permitted (CC BY-ND 4.0) | Feed unverified | search |
| IFS | Econ | Think-tank reports | Restricted (all rights reserved; by permission) | Site answers 403 to fetches | search |
| Brookings | Econ | Think-tank reports | Forbidden (no robots or data mining) | Feed unverified | read |
| PIIE | Econ | Think-tank reports | Forbidden (no automated tools without permission) | Feed unverified | read |
| arXiv | Research | Preprints | Metadata permitted (CC0) and personal use; full text by each paper's licence | Atom/RSS per category (daily), API one request per 3 s, OAI-PMH | read |
| AQR | Research | Firm research, blog | Forbidden without written consent | No API; RSS mentioned, URL not found | read |
| NBER | Research | Working papers | Restricted (personal use; not "en masse") | RSS URLs disputed | read |
| RePEc / IDEAS | Research | Metadata index | Restricted (non-commercial; no scraping; no full text) | OAI-PMH, rsync | read |
| NASA ADS | Research | Abstracts and articles index | Restricted (no systematic downloads) | API with token | read |
| SSRN, CEPR/VoxEU | Research | Working papers, columns | Unclear (terms pages answered 403) | Unknown | none |

**Verdicts** are for go-slow's use: collected on a timetable, full text kept, a model reading it, one private reader. *Permitted*: the terms or an open licence grant it. *Restricted*: personal use allowed, with conditions, or silent on automated access or derivatives. *Forbidden*: barred without permission. *Unclear*: unread, contested, or no licence stated.

**Volume.** arXiv took 2,265 q-fin, 2,313 econ and 18,660 astro-ph papers in 2025, plus cross-lists: about six q-fin and six econ a day, but around 70 astro-ph each weekday, too many for one reader without narrowing to subclasses or a Topic.

## Grounding

Researched 2026-10-29 by four delegated researchers (space; official economics bodies; non-official economics and markets; long-form research), then merged. Briefed from the Owner's questions: the interests, the two named publishers (arXiv, AQR) and go-slow's use; no candidate list was given beyond those. Evidence is tagged as in the table: **read** is the publisher's own page fetched in this pass (through a tool that summarises pages, so quotes are as it returned them); **search** is search snippets or third-party pages; **recall** is unchecked prior knowledge.

Primary pages read: [NASA usage guidelines](https://www.nasa.gov/nasa-brand-center/images-and-media/), [ESA terms](https://www.esa.int/Services/Terms_and_conditions) and [RSS feeds](https://www.esa.int/Services/RSS_Feeds), [JAXA site policy](https://global.jaxa.jp/policy.html), [GCAT](https://planet4589.org/space/gcat/), [Launch Library 2](https://thespacedevs.com/llapi), [Spaceflight News API](https://www.spaceflightnewsapi.net/), [CelesTrak](https://celestrak.org/NORAD/documentation/gp-data-formats.php), [Space-Track user agreement](https://www.space-track.org/documentation#/user_agree), [NOAA SWPC](https://www.spaceweather.gov/content/subscription-services), [Federal Reserve disclaimer](https://www.federalreserve.gov/disclaimer.htm) and [feeds](https://www.federalreserve.gov/feeds/feeds.htm), [ECB copyright](https://www.ecb.europa.eu/services/disclaimer/html/index.en.html) and [RSS](https://www.ecb.europa.eu/home/html/rss.en.html), [Bank of England legal](https://www.bankofengland.co.uk/legal) and [RSS](https://www.bankofengland.co.uk/rss), [OBR copyright](https://obr.uk/copyright/), [BIS terms](https://www.bis.org/terms_conditions.htm), [World Bank terms](https://www.worldbank.org/en/about/legal/terms-of-use-for-datasets), [IMF terms](https://www.imf.org/en/About/copyright-and-terms), [FCA legal](https://www.fca.org.uk/legal), [SEC accessing EDGAR](https://www.sec.gov/search-filings/edgar-search-assistance/accessing-edgar-data), [Companies House developer guidelines](https://developer.company-information.service.gov.uk/developer-guidelines), [Resolution Foundation copyright](https://www.resolutionfoundation.org/privacy/copyright/), [Brookings terms](https://www.brookings.edu/terms-of-use/), [PIIE terms](https://www.piie.com/terms-use), [arXiv API terms](https://info.arxiv.org/help/api/tou.html), [bulk data](https://info.arxiv.org/help/bulk_data.html), [licences](https://info.arxiv.org/help/license/index.html), [RSS](https://info.arxiv.org/help/rss.html) and yearly counts ([q-fin](https://arxiv.org/year/q-fin/2025), [econ](https://arxiv.org/year/econ/2025), [astro-ph](https://arxiv.org/year/astro-ph/2025)), [AQR terms](https://www.aqr.com/Terms-of-Use), [NBER for libraries](https://www.nber.org/subscribe/information-libraries), [IDEAS getdata](https://ideas.repec.org/getdata.html), [NASA ADS terms](https://adsabs.github.io/help/terms/); the GOV.UK Atom feeds for the UK Space Agency and HM Treasury, fetched directly.

From search only: [Eurostat copyright](https://ec.europa.eu/eurostat/help/copyright-notice) and [API](https://ec.europa.eu/eurostat/web/user-guides/data-browser/api-data-access), [OECD open by default](https://oecd.org/en/about/oecd-open-by-default-policy.html), [Bruegel](https://www.bruegel.org/zh-hant/node/90), [IFS copyright policy](https://ifs.org.uk/sites/default/files/2024-01/Copyright%20and%20permissions%20policy.pdf), Companies House's licence ([FOI 362](https://assets.publishing.service.gov.uk/media/5a7f6e7f40f0b6230268f75c/Disc_Log_362__Enquiry_regarding_Companies_House_Free_Company_Data_Product-ver0.2.pdf), [forum](https://forum.companieshouse.gov.uk/t/is-data-provided-under-the-ocs/4513)), the [FCA NSM terms](https://data.fca.org.uk/artefacts/NSM_Terms_of_Use.pdf), FAA data ([Fact Book](https://www.faa.gov/newsroom/faa-fact-book-explore-commercial-space-data), [spaceports](https://catalog.data.gov/dataset/spaceports)).

**Not read** (verdicts there rest on a gap, not a finding): SpaceNews (429), the UK CAA registers (redirect loop), CelesTrak's usage policy, the OECD's current terms (403), BIS's and the World Bank's data terms, IFS (403), the FCA NSM acceptable use policy, NBER's terms (404), SSRN and CEPR (403), the ADS AI policy, arXiv's statement on full-text reuse beyond the pages above. No robots.txt was checked (Collection's own daily check settles that), and **for no publisher was it checked whether feed items carry full text**.

**Not reached:** space agencies beyond NASA, ESA, JAXA and the UK (ISRO, CNES, DLR, CSA, CNSA, KARI); FCC, Ofcom and ITU filings; other launch schedules; scientific bodies (RAS, IAU, Minor Planet Center); every company newsroom and independent space outlet; regional Federal Reserve Banks, BLS, BEA, US Treasury; other central banks and statistics offices; ESMA, EBA, WTO, UNCTAD; most UK and European think tanks (NIESR, CEP, IfG, CEPS, Kiel, Ifo); newsletters and blogs; free market-data APIs; asset managers' and banks' research beyond AQR; open-access journals.

**Where the sources disagreed with recall:** recall put arXiv's q-fin and econ at about ten papers a weekday each; the counts give about six a day. Recall allowed that AQR's posts might be kept as a summary and link; its terms forbid copying any information from the site. Recall had the UK Space Agency folded into its department and its feed perhaps quiet; the feed is live, posting several times a week.
