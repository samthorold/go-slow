package sqlite

// The Feedback kept before the Record became events (the feedback table):
// none is given so now. A piece on a Story, a Development, a Fact or a
// Connection made marks (withdrawn_by, unwanted_by, missed_by) that stay as
// they were, read by the pages of what they marked; the register still
// counts every piece, those on a Sunday read of the earlier shape and its
// Assessments among them.

// addedByUndone is whether a Development, or a Story, was added by a
// missed piece since undone: it is no longer added, and is shown nowhere.
const (
	addedByUndone      = `EXISTS (SELECT 1 FROM feedback u WHERE u.id = developments.missed_by AND u.undone_at IS NOT NULL)`
	storyAddedByUndone = `EXISTS (SELECT 1 FROM feedback u WHERE u.id = stories.missed_by AND u.undone_at IS NOT NULL)`
)

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
