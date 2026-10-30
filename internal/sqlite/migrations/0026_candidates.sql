-- Candidate judges, as events (system design: Every judgement carries its
-- judge, and a changed judge judges forward). A candidate (kind 'candidate')
-- is a run judging a range of the Record again, as the act, the version of
-- its instructions and the model: its judgements are kept beside the live
-- ones and read by no view until it is chosen. The range is the Items
-- published from range_from up to, not including, range_to (RFC 3339 UTC).
CREATE TABLE candidates (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    act TEXT NOT NULL,
    version INTEGER NOT NULL,
    model TEXT NOT NULL,
    range_from TEXT NOT NULL,
    range_to TEXT NOT NULL
) STRICT;

-- The readings a candidate made: part of each reading's event, written with
-- it. A reading not listed here was made as the live judge.
CREATE TABLE candidate_readings (
    reading_id INTEGER PRIMARY KEY REFERENCES readings (event_id),
    candidate_id INTEGER NOT NULL REFERENCES candidates (event_id)
) STRICT;

CREATE INDEX candidate_readings_by_candidate ON candidate_readings (candidate_id);

-- Choosing a candidate (kind 'choice'): from the choice on, the candidate is
-- a live judge, and its judgements are read as made then, newer than every
-- live judgement made before it. Nothing it judged is rewritten. A candidate
-- is chosen at most once.
CREATE TABLE candidate_choices (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    candidate_id INTEGER NOT NULL UNIQUE REFERENCES candidates (event_id)
) STRICT;

CREATE TRIGGER candidates_not_rewritten BEFORE UPDATE ON candidates
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER candidates_not_removed BEFORE DELETE ON candidates
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER candidate_readings_not_rewritten BEFORE UPDATE ON candidate_readings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER candidate_readings_not_removed BEFORE DELETE ON candidate_readings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER candidate_choices_not_rewritten BEFORE UPDATE ON candidate_choices
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER candidate_choices_not_removed BEFORE DELETE ON candidate_choices
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
