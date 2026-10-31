-- The Feedback a judgement's judge was shown as precedent, kept with the
-- judgement (a reading, a relating, a Briefing), in the order shown (system
-- design: Feedback becomes precedent, shown as cases), so why a judge judged
-- as it did can be read later from what it was shown. Part of the
-- judgement's event: never rewritten or removed.
CREATE TABLE judgement_precedents (
    judgement_id INTEGER NOT NULL REFERENCES events (id),
    rank INTEGER NOT NULL,
    feedback_id INTEGER NOT NULL REFERENCES feedback_given (event_id),
    PRIMARY KEY (judgement_id, rank)
) STRICT;

CREATE TRIGGER judgement_precedents_not_rewritten BEFORE UPDATE ON judgement_precedents
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER judgement_precedents_not_removed BEFORE DELETE ON judgement_precedents
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
