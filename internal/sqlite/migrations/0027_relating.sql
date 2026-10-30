-- Relating an Item's Spans, as events (system design: Relating a Span
-- judges follows, same as and contradicts in one call, once per pair). A
-- relating (kind 'relating') is the act run on an Item read as one of its
-- readings, with its Judge: the act, its instructions' version, the model
-- that answered and the hash of the request it answered (the hash a kept
-- call carries); an Item whose Spans had no candidates is related with no
-- call, and its model and request hash are ''. Each relation it judged is an
-- event of its own (kind 'relation'), tied to it: from a Span of one Item to
-- a Span of another, from the later-published to the earlier, as byte
-- offsets into each Item's text as kept. One of the two is always the
-- relating's own Item.
CREATE TABLE relatings (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    reading_id INTEGER NOT NULL REFERENCES readings (event_id),
    act TEXT NOT NULL,
    version INTEGER NOT NULL,
    model TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX relatings_by_item ON relatings (source_id, entry_id, event_id);

CREATE TABLE relations (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    relating_id INTEGER NOT NULL REFERENCES relatings (event_id),
    kind TEXT NOT NULL CHECK (kind IN ('follows', 'same_as', 'contradicts')),
    from_source_id TEXT NOT NULL,
    from_entry_id TEXT NOT NULL,
    from_start INTEGER NOT NULL,
    from_end INTEGER NOT NULL,
    to_source_id TEXT NOT NULL,
    to_entry_id TEXT NOT NULL,
    to_start INTEGER NOT NULL,
    to_end INTEGER NOT NULL,
    CHECK (0 <= from_start AND from_start < from_end),
    CHECK (0 <= to_start AND to_start < to_end),
    CHECK (from_source_id != to_source_id OR from_entry_id != to_entry_id),
    FOREIGN KEY (from_source_id, from_entry_id) REFERENCES items (source_id, entry_id),
    FOREIGN KEY (to_source_id, to_entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX relations_by_relating ON relations (relating_id);
CREATE INDEX relations_from ON relations (from_source_id, from_entry_id, from_start, from_end);
CREATE INDEX relations_to ON relations (to_source_id, to_entry_id, to_start, to_end);

CREATE TRIGGER relatings_not_rewritten BEFORE UPDATE ON relatings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER relatings_not_removed BEFORE DELETE ON relatings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER relations_not_rewritten BEFORE UPDATE ON relations
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER relations_not_removed BEFORE DELETE ON relations
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

-- Placing and connecting are gone: relating replaces them. Their work
-- awaits nothing now, so its failures are no longer listed or retried.
DELETE FROM work_attempts WHERE act IN ('placing', 'connecting');
