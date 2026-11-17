# Collection fixtures

Full-text documents Collection fetches for an entry whose feed carries only a summary or an opening, each as the publisher's API or site returned it, and recorded feeds.

| File | Document |
|---|---|
| `fed-press-releases.rss` | The Federal Reserve Board's press release feed (`https://www.federalreserve.gov/feeds/press_all.xml`), as it was returned on 17 November 2026, trimmed to one of its twenty items and to no trailing whitespace on any line (its byte order mark kept) |
| `fed-speeches.rss` | The Board's speeches feed (`https://www.federalreserve.gov/feeds/speeches.xml`), as it was returned on 17 November 2026, trimmed as the press release feed is, to one of its fifteen items |
| `fed-monetary20261112a.htm` | The press release *Federal Reserve announces revised tentative schedule of Federal Open Market Committee meetings in 2027*, 12 November 2026, its page (`https://www.federalreserve.gov/newsevents/pressreleases/monetary20261112a.htm`) as it was returned on 17 November 2026, trimmed to the shape around its article body (most of the navigation, scripts and footer, and the sharing links but one, removed; `div#article` and the article body whole) and to no trailing whitespace on any line |
| `fed-waller20261117a.htm` | Governor Waller's speech *Considerations at the Intersection of Payments and Tokenized Markets*, 17 November 2026, its page (`https://www.federalreserve.gov/newsevents/speech/waller20261117a.htm`) as it was returned on 17 November 2026, trimmed as the release's page is, and its article body to its first and last paragraphs and first two footnotes |
| `govuk-hm-treasury.atom` | HM Treasury's GOV.UK Atom feed (`https://www.gov.uk/government/organisations/hm-treasury.atom`), as it was returned on 29 October 2026: twenty entries from the days around the Budget |
| `govuk-budget-2026-speech.json` | Budget 2026 speech, 28 October 2026, as the GOV.UK Content API (`https://www.gov.uk/api/content/<path>`) returned it on 29 October 2026 |
| `govuk-appointment-of-lord-lieutenant-of-county-tyrone-15-october-2026.json` | Appointment of Lord-Lieutenant of County Tyrone, 15 October 2026, as the GOV.UK Content API (`https://www.gov.uk/api/content/<path>`) returned it on 25 October 2026 (the same document as `internal/editor/testdata/govuk/`) |
| `nasa-news-releases.rss` | NASA's news release feed (`https://www.nasa.gov/news-release/feed/`), as it was returned on 17 November 2026, trimmed to four of its ten items (two releases, an image post and an APOD entry) and to no trailing whitespace on any line |
| `written-statement-1952546.json` | Written statement HLWS419, Small Business Regulatory Taskforce: Government Response, 23 October 2026, as the Written Questions Service API (`https://questions-statements-api.parliament.uk/api/writtenstatements/statements/1952546`) returned it on 25 October 2026 |

Contains public sector information licensed under the Open Government Licence v3.0 (https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/).

Contains Parliamentary information licensed under the Open Parliament Licence v3.0 (https://www.parliament.uk/site-information/copyright-parliament/open-parliament-licence/).

Source: NASA (www.nasa.gov), under its media usage guidelines (https://www.nasa.gov/nasa-brand-center/images-and-media/).

Source: Board of Governors of the Federal Reserve System (www.federalreserve.gov), in the public domain under its website disclaimer (https://www.federalreserve.gov/disclaimer.htm).
