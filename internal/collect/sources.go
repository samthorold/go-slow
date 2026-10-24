package collect

import "time"

// Sources is the list of Sources the Owner maintains by hand. Topics never
// add to it (system design: The owner chooses the sources; topics don't).
func Sources() []Source {
	return []Source{
		{
			// Number 10's own announcements: the Prime Minister's
			// statements, speeches, calls and appointments, as published,
			// which reporting on UK politics most often goes back to.
			ID:          "govuk-number-10",
			Name:        "Prime Minister's Office, 10 Downing Street (GOV.UK)",
			FeedURL:     "https://www.gov.uk/government/organisations/prime-ministers-office-10-downing-street.atom",
			Attribution: OGL,
			// It publishes most weekdays; four days covers a weekend and a
			// bank holiday.
			UsualGap: 4 * 24 * time.Hour,
		},
	}
}
