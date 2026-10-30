-- Reading an Item, as events (system design: Reading an Item makes
-- construction's model judgements in one call). A reading (kind 'reading')
-- is the act run on an Item shown as one of its Cuts, with its Judge: the
-- act, its instructions' version, the model that answered and the hash of
-- the request it answered (the hash a kept call carries). Each judgement it
-- made is an event of its own (kinds 'refers_to', 'name', 'speaker',
-- 'rejected'), tied to its reading and so to the reading's Judge, so a view
-- can take one kind from one Judge and another from another. Every address
-- is into the reading's Item, as byte offsets into its text as kept, start
-- inclusive and end exclusive, so a judgement can't address another Item.
CREATE TABLE readings (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    cut_id INTEGER NOT NULL REFERENCES cuts (event_id),
    act TEXT NOT NULL,
    version INTEGER NOT NULL,
    model TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX readings_by_item ON readings (source_id, entry_id, event_id);

-- A Span refers to another of the same Item: from names in other words what
-- to names.
CREATE TABLE refers_to (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    reading_id INTEGER NOT NULL REFERENCES readings (event_id),
    from_start INTEGER NOT NULL,
    from_end INTEGER NOT NULL,
    to_start INTEGER NOT NULL,
    to_end INTEGER NOT NULL,
    CHECK (0 <= from_start AND from_start < from_end),
    CHECK (0 <= to_start AND to_start < to_end)
) STRICT;

CREATE INDEX refers_to_by_reading ON refers_to (reading_id);

-- A Span names an Entity: the name the Item gives it in full, its kind, and
-- for a person the office the Item gives them ('' for none).
CREATE TABLE span_names (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    reading_id INTEGER NOT NULL REFERENCES readings (event_id),
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('person', 'organisation', 'place', 'occasion')),
    office TEXT NOT NULL,
    CHECK (0 <= span_start AND span_start < span_end),
    CHECK (office = '' OR kind = 'person')
) STRICT;

CREATE INDEX span_names_by_reading ON span_names (reading_id);

-- A Span is the words of whoever the speaker Span names.
CREATE TABLE speakers (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    reading_id INTEGER NOT NULL REFERENCES readings (event_id),
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    speaker_start INTEGER NOT NULL,
    speaker_end INTEGER NOT NULL,
    CHECK (0 <= span_start AND span_start < span_end),
    CHECK (0 <= speaker_start AND speaker_start < speaker_end)
) STRICT;

CREATE INDEX speakers_by_reading ON speakers (reading_id);

-- A Span judged not useful for editorial work.
CREATE TABLE rejections (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    reading_id INTEGER NOT NULL REFERENCES readings (event_id),
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    CHECK (0 <= span_start AND span_start < span_end)
) STRICT;

CREATE INDEX rejections_by_reading ON rejections (reading_id);

CREATE TRIGGER readings_not_rewritten BEFORE UPDATE ON readings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER readings_not_removed BEFORE DELETE ON readings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER refers_to_not_rewritten BEFORE UPDATE ON refers_to
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER refers_to_not_removed BEFORE DELETE ON refers_to
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER span_names_not_rewritten BEFORE UPDATE ON span_names
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER span_names_not_removed BEFORE DELETE ON span_names
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER speakers_not_rewritten BEFORE UPDATE ON speakers
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER speakers_not_removed BEFORE DELETE ON speakers
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER rejections_not_rewritten BEFORE UPDATE ON rejections
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER rejections_not_removed BEFORE DELETE ON rejections
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

-- The attempts counted against the earlier reading act, which drew Facts:
-- reading is a new act, so its work starts with all its attempts, and an
-- Item that failed the earlier act is read like any other.
DELETE FROM work_attempts WHERE act = 'reading';
