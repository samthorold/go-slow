-- The effort a request asks the model for is part of the request as sent
-- (editor.Request.Effort), so a kept call and a pending batch keep it beside
-- its model: '' for a request that asks for none, as every request before
-- weaving did.
ALTER TABLE model_calls ADD COLUMN effort TEXT NOT NULL DEFAULT '';
ALTER TABLE batch_requests ADD COLUMN effort TEXT NOT NULL DEFAULT '';

-- Weaving (system design: Weaving asks for an editor's opinion, and need get
-- none). A weaving run whose result was written is an event (kind
-- 'weaving'), keyed by the day it was submitted on (work_key, as its calls
-- and attempts are), with the newest Cut whose Story it was shown
-- (through_cut: the next run is shown the Stories cut after it) and its
-- Judge. A run that made no Stories is kept all the same. Each Story it made
-- is a Judgement of its own (kind 'story', table woven_stories), tied to it; the Story holds the
-- Editor's Spans it wrote, numbered n from 1 in the order written, each with
-- its words (an Editor's Span is kept in the Judgement that wrote it), and the
-- Spans each cites, numbered m from 1 in the order cited, each an address
-- into its Item's text as kept.
CREATE TABLE weavings (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    work_key TEXT NOT NULL,
    through_cut INTEGER NOT NULL REFERENCES cuts (event_id),
    act TEXT NOT NULL,
    version INTEGER NOT NULL,
    model TEXT NOT NULL,
    request_hash TEXT NOT NULL
) STRICT;

CREATE INDEX weavings_by_key ON weavings (work_key);

-- Named woven_stories, not stories: the old design's stories table, dropped
-- by 0032, is a name the Record keeps out.
CREATE TABLE woven_stories (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    weaving_id INTEGER NOT NULL REFERENCES weavings (event_id)
) STRICT;

CREATE INDEX woven_stories_by_weaving ON woven_stories (weaving_id);

CREATE TABLE editor_spans (
    story_id INTEGER NOT NULL REFERENCES woven_stories (event_id),
    n INTEGER NOT NULL,
    body TEXT NOT NULL CHECK (body != ''),
    PRIMARY KEY (story_id, n)
) STRICT;

CREATE TABLE editor_span_cites (
    story_id INTEGER NOT NULL,
    n INTEGER NOT NULL,
    m INTEGER NOT NULL,
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    PRIMARY KEY (story_id, n, m),
    CHECK (0 <= span_start AND span_start < span_end),
    FOREIGN KEY (story_id, n) REFERENCES editor_spans (story_id, n),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX editor_span_cites_by_span ON editor_span_cites (source_id, entry_id, span_start, span_end);

CREATE TRIGGER weavings_not_rewritten BEFORE UPDATE ON weavings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER weavings_not_removed BEFORE DELETE ON weavings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER woven_stories_not_rewritten BEFORE UPDATE ON woven_stories
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER woven_stories_not_removed BEFORE DELETE ON woven_stories
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER editor_spans_not_rewritten BEFORE UPDATE ON editor_spans
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER editor_spans_not_removed BEFORE DELETE ON editor_spans
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER editor_span_cites_not_rewritten BEFORE UPDATE ON editor_span_cites
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER editor_span_cites_not_removed BEFORE DELETE ON editor_span_cites
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
