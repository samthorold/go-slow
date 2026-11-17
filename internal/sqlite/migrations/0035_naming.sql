-- Naming a Story (system design: Every Story the Editor makes has a Name,
-- and nothing reads it but the Owner). A Name is a Judgement of its own, an
-- event (kind 'story_name'; 'name' is what a reading says a Span names), naming one Story the Editor made, with its words and
-- its Judge. A Story is read with its newest Name, so a new Judge's naming
-- stands beside the earlier one rather than rewriting it.
CREATE TABLE story_names (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    story_id INTEGER NOT NULL REFERENCES woven_stories (event_id),
    name TEXT NOT NULL CHECK (name != ''),
    act TEXT NOT NULL,
    version INTEGER NOT NULL,
    model TEXT NOT NULL,
    request_hash TEXT NOT NULL
) STRICT;

CREATE INDEX story_names_by_story ON story_names (story_id, event_id);

CREATE TRIGGER story_names_not_rewritten BEFORE UPDATE ON story_names
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER story_names_not_removed BEFORE DELETE ON story_names
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
