package collect

import "time"

// The licences the Sources are collected under. Each lets the Record keep
// an Item's full text, with attribution, and excludes personal data, logos
// and third-party material (sources research).
const (
	OGLv3 = "Open Government Licence v3.0"
	OPLv3 = "Open Parliament Licence v3.0"
)

// NASA's terms, from its media usage guidelines
// (https://www.nasa.gov/nasa-brand-center/images-and-media/, read
// 2026-11-17): NASA content is generally not subject to copyright in the
// United States, news outlets may use it without explicit permission in a
// factual manner that implies no endorsement, NASA should be acknowledged
// as its source, and third parties' copyright material on its site is
// marked as such and not covered. The page speaks of media; that a
// release's text is a US government work, in the public domain, is the
// sources research's reading (docs/research/197-more-sources.md), not a
// sentence of the page.
const (
	NASAGuidelines = "NASA media usage guidelines (US government work, not subject to copyright in the US)"
	NASACredit     = "Source: NASA (www.nasa.gov)."
)

// ons is the ONS API's release search; the two ONS Sources ask it for
// published releases and for upcoming ones.
const ons = "https://api.beta.ons.gov.uk/v1/search/releases"

// Sources is the list of Sources the Owner maintains by hand. Topics never
// add to it (system design: The owner chooses the sources; topics don't).
func Sources() []Source {
	return []Source{
		{
			// Number 10's own announcements: the Prime Minister's
			// statements, speeches, calls and appointments, as published,
			// which reporting on UK politics most often goes back to.
			ID:        "govuk-number-10",
			Name:      "Prime Minister's Office, 10 Downing Street (GOV.UK)",
			ShortName: "No 10",
			FeedURL:   "https://www.gov.uk/government/organisations/prime-ministers-office-10-downing-street.atom",
			Format:    Atom,
			// Its entries carry only a summary; the text is the Content
			// API document for each entry's path.
			Text:        TextFromGOVUKContent,
			Licence:     OGLv3,
			Attribution: OGL,
			Keeps:       KeepFullText,
			// It publishes most weekdays; four days covers a weekend and a
			// bank holiday.
			UsualGap: 4 * 24 * time.Hour,
			// GOV.UK answers conditional requests, so an unchanged feed
			// costs it a 304 on every run.
		},
		{
			// The UK Space Agency's news, guidance and research: the UK's
			// civil space programme, launches and missions, as it
			// publishes them, the first Source for space.
			ID:        "govuk-uk-space-agency",
			Name:      "UK Space Agency (GOV.UK)",
			ShortName: "UK Space Agency",
			FeedURL:   "https://www.gov.uk/government/organisations/uk-space-agency.atom",
			Format:    Atom,
			// As Number 10's, its entries carry only a summary; the text
			// is the Content API document for each entry's path.
			Text:        TextFromGOVUKContent,
			Licence:     OGLv3,
			Attribution: OGL,
			Keeps:       KeepFullText,
			// It posts several times a week, but its feed on 2026-10-29
			// showed a gap of twelve days (1 to 13 October 2026); two
			// weeks keeps such a lull from showing it quiet.
			UsualGap: 14 * 24 * time.Hour,
		},
		{
			// HM Treasury's publications: the Chancellor's speeches and
			// statements, the Budget and other fiscal events, Treasury
			// statistics (such as the GDP deflators), consultations and
			// policy papers.
			ID:        "govuk-hm-treasury",
			Name:      "HM Treasury (GOV.UK)",
			ShortName: "Treasury",
			FeedURL:   "https://www.gov.uk/government/organisations/hm-treasury.atom",
			Format:    Atom,
			// As Number 10's, its entries carry only a summary; the text is
			// the Content API document for each entry's path.
			Text:        TextFromGOVUKContent,
			Licence:     OGLv3,
			Attribution: OGL,
			Keeps:       KeepFullText,
			// It publishes most weekdays, but less steadily than Number 10:
			// from May to October 2026 (GOV.UK's search, 29 October 2026)
			// it went four or five days without publishing several times,
			// and six once, in August. Seven days covers those; a longer
			// gap is worth noticing.
			UsualGap: 7 * 24 * time.Hour,
		},
		{
			// NASA's news releases: the agency's own announcements of
			// missions, launches, contracts and results, as published, the
			// first Source for space outside the UK.
			ID:        "nasa-news-releases",
			Name:      "NASA: news releases",
			ShortName: "NASA",
			// The feed NASA lists as its news releases, which also carries
			// image posts, APOD and other articles (2026-11-17: two
			// releases among ten items). No feed carries the releases
			// alone (asking its feed for ?post_type=press-release answers
			// the same mix); the WordPress API does, but the feed serves,
			// read for the entries linking under /news-release/, which are
			// NASA's releases and nothing else.
			FeedURL:    "https://www.nasa.gov/news-release/feed/",
			Format:     RSS,
			LinkPrefix: "https://www.nasa.gov/news-release/",
			// Each item carries its full text in content:encoded.
			Text:        TextInEntry,
			Licence:     NASAGuidelines,
			Attribution: NASACredit,
			Keeps:       KeepFullText,
			// About one release a day: the hundred before 17 November 2026
			// (its WordPress API, from 20 July) went at most nine days and
			// nineteen hours without one (31 July to 10 August). Twelve
			// days keeps such a lull from showing it quiet.
			UsualGap: 12 * 24 * time.Hour,
			// It answers conditional requests (ETag, 304), so an unchanged
			// feed costs it a 304 on every run; its Cache-Control max-age
			// is 300 seconds, shorter than Collection's interval.
		},
		{
			// Written ministerial statements to both Houses: government
			// announcements made to Parliament, as made. Read through the
			// Written Questions Service API, never the main site.
			ID:        "parliament-written-statements",
			Name:      "UK Parliament: written statements",
			ShortName: "Parliament",
			FeedURL:   "https://questions-statements-api.parliament.uk/api/writtenstatements/statements?take=50",
			Format:    WrittenStatements,
			// The list carries only each statement's opening; the text is
			// the statement itself, from the same API.
			Text:        TextFromWrittenStatement,
			Licence:     OPLv3,
			Attribution: OPL,
			Keeps:       KeepFullText,
			// Statements are made on sitting days. Ten days covers a short
			// recess; the summer recess shows it quiet, as it is.
			UsualGap: 10 * 24 * time.Hour,
			// It sends no validators to make a request conditional, only
			// max-age=60; hourly is as fresh as a slow briefing needs.
			MinInterval: time.Hour,
		},
		{
			// Every ONS release as it is published: GDP, inflation, the
			// labour market and the rest of the official statistics.
			ID:          "ons-releases",
			Name:        "Office for National Statistics: releases",
			ShortName:   "ONS",
			FeedURL:     ons + "?release-type=type-published&sort=release_date_desc&limit=50",
			Format:      ONSReleases,
			Licence:     OGLv3,
			Attribution: OGL,
			Keeps:       KeepFullText,
			// It publishes most weekdays.
			UsualGap: 4 * 24 * time.Hour,
			// The API sends no validators; releases come out at fixed
			// times in the morning, so hourly misses none for long.
			MinInterval: time.Hour,
		},
		{
			// The ONS release calendar: releases scheduled but not yet
			// published, each a scheduled event, due on its release date.
			ID:          "ons-release-calendar",
			Name:        "Office for National Statistics: release calendar",
			ShortName:   "ONS",
			FeedURL:     ons + "?release-type=type-upcoming&sort=release_date_asc&limit=100",
			Format:      ONSReleaseCalendar,
			Licence:     OGLv3,
			Attribution: OGL,
			Keeps:       KeepFullText,
			// The next hundred releases cover a few weeks, and new ones
			// come into it as earlier ones are published.
			UsualGap: 7 * 24 * time.Hour,
			// The calendar changes slowly and the API sends no validators.
			MinInterval: 6 * time.Hour,
		},
	}
}
