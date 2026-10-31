-- Feedback on judgements, as events (system design: Feedback lands on a
-- judgement, so the judge to fix is known; Feedback is kept as given, and
-- undoing it is Feedback too). A piece (kind 'feedback') is what the Owner
-- said, about what, and when (the event's at), and nothing of the state it
-- was given against:
--   wrong and new to me, on a judgement: an event of the judgement's own
--   kind (judgement_id);
--   unwanted, on a Span as told in a Briefing (briefing_id and the Span);
--   missed, on a Span the Briefing should have told (the Span).
-- Undoing a piece (kind 'undoing') is an event of its own, naming the piece:
-- the piece stays, and stops standing. A piece is undone at most once.
-- The Feedback kept before (the feedback table) is read as it was, until
-- its tables go.
CREATE TABLE feedback_given (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    kind TEXT NOT NULL CHECK (kind IN ('wrong', 'unwanted', 'missed', 'new to me')),
    judgement_id INTEGER REFERENCES events (id),
    briefing_id INTEGER REFERENCES told_briefings (event_id),
    source_id TEXT,
    entry_id TEXT,
    span_start INTEGER,
    span_end INTEGER,
    CHECK ((kind IN ('wrong', 'new to me')) = (judgement_id IS NOT NULL)),
    CHECK ((kind = 'unwanted') = (briefing_id IS NOT NULL)),
    CHECK ((kind IN ('unwanted', 'missed')) = (source_id IS NOT NULL AND entry_id IS NOT NULL
        AND span_start IS NOT NULL AND span_end IS NOT NULL)),
    CHECK (span_start IS NULL OR (0 <= span_start AND span_start < span_end)),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX feedback_given_by_judgement ON feedback_given (judgement_id);
CREATE INDEX feedback_given_by_span ON feedback_given (source_id, entry_id, span_start, span_end);

CREATE TABLE feedback_undoings (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    feedback_id INTEGER NOT NULL UNIQUE REFERENCES feedback_given (event_id)
) STRICT;

CREATE TRIGGER feedback_given_not_rewritten BEFORE UPDATE ON feedback_given
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER feedback_given_not_removed BEFORE DELETE ON feedback_given
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER feedback_undoings_not_rewritten BEFORE UPDATE ON feedback_undoings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER feedback_undoings_not_removed BEFORE DELETE ON feedback_undoings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
