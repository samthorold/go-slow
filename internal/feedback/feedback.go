// Package feedback is the Owner's Feedback: what they tell the Editor,
// kept as events (Piece, Pieces: system design: Feedback lands on a
// judgement, so the judge to fix is known; Feedback is kept as given, and
// undoing it is Feedback too).
//
// A piece (judgements.go; stored by internal/sqlite/pieces.go, migration
// 0029_feedback_events.sql: event kind feedback, table feedback_given) is
// its kind, what it lands on and its time, nothing of the state it was
// given against:
//   - wrong on a judgement's event (judgement_id: a refers_to, a lookup (a
//     name's Entity match), a speaker, a relation, a gathering, a
//     statement, or an assessment of a Sunday read);
//   - new to me on a statement, a gathering, an assessment, or a
//     sunday_read as a whole;
//   - unwanted on a Span as told in a Briefing (briefing_id and the Span);
//   - missed on a Span.
//
// Undoing is an event of its own (undoing, table feedback_undoings), never
// an edit: the piece stays, and stops standing. Pieces.Give takes an Ask
// (the form's kind, judgement, span "start-end" and the page's Item, and the
// Briefing's day on a Briefing's page) and refuses with Errors what the
// design doesn't give where it was asked (a kind on a judgement it doesn't
// take, unwanted off a Briefing or on a Span it didn't tell that day, missed
// from a Briefing's page); a judgement or Span not in the Record is
// ErrNotInRecord.
//
// Nothing is applied: every view works out what it reads from the Feedback
// standing (wrongStanding in the storage adapter). NewestReading leaves out
// a refers_to or speaker under it (so the Span no longer mentions an Entity
// through it); EntitiesNamed reads the newest look-up of a key not under it
// (the key is not looked up again); RelationsOf and LatestGathering leave out
// a relation or gathering under it; the told Briefing's statements carry
// TakenOutBy (wrong on it, unwanted on a Span it cites as told that day),
// which its page leaves out. Wrong on an Assessment takes it out of every
// later read's assessments tool and the statements citing it out of its
// read's page. KeptPieces is each piece with what it is about (What, the
// Spans it lands on, a look-up's key; for a Sunday read, Read and
// Conversation) and the calls behind it, worked out by the things' own
// lookups. Feedback carries no Depth.
//
// The Feedback kept before the Record kept only events went with the old
// design's judgements (0032_events_only.sql). Undone Feedback is never
// precedent and the register does not count it.
package feedback

import (
	"errors"
)

// Kind is what the Owner says.
type Kind string

// The four kinds of Feedback.
const (
	// Missed: a Span should have been told.
	Missed Kind = "missed"
	// Unwanted: a Span told shouldn't have been.
	Unwanted Kind = "unwanted"
	// Wrong: a judgement is mistaken.
	Wrong Kind = "wrong"
	// NewToMe: something told showed the Owner what they hadn't seen.
	NewToMe Kind = "new to me"
)

// Errors are validation failures, by field; "" is for the form as a whole.
type Errors map[string]string

// ErrNotInRecord is the error for Feedback about something not in the
// Record.
var ErrNotInRecord = errors.New("not in the Record")
