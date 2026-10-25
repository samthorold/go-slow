package collect

import "time"

// The licences the Sources are collected under. Each lets the Record keep
// an Item's full text, with attribution, and excludes personal data, logos
// and third-party material (sources research).
const (
	OGLv3 = "Open Government Licence v3.0"
	OPLv3 = "Open Parliament Licence v3.0"
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
			ID:      "govuk-number-10",
			Name:    "Prime Minister's Office, 10 Downing Street (GOV.UK)",
			FeedURL: "https://www.gov.uk/government/organisations/prime-ministers-office-10-downing-street.atom",
			Format:  Atom,
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
			// Written ministerial statements to both Houses: government
			// announcements made to Parliament, as made. Read through the
			// Written Questions Service API, never the main site.
			ID:      "parliament-written-statements",
			Name:    "UK Parliament: written statements",
			FeedURL: "https://questions-statements-api.parliament.uk/api/writtenstatements/statements?take=50",
			Format:  WrittenStatements,
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
