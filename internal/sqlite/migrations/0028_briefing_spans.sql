-- The Briefing over Spans, as events (system design: The Briefing tells
-- what the Editor judges worth telling; A Story is gathered by the pass that
-- tells it). A Briefing (kind 'briefing') is the Editor's judgement for a
-- day, with its Judge: the act, its instructions' version, the model that
-- answered and the hash of the request it answered (the hash a kept call
-- carries); one written empty, with no call, has '' for both. Its day is in
-- briefings, which keeps every day a Briefing was written for, in either
-- shape. Each statement it makes (kind 'statement') and each Story it
-- gathers (kind 'gathering') is an event of its own, tied to it, so
-- Feedback can land on each. Every address is a Span: byte offsets into its
-- Item's text as kept, start inclusive and end exclusive.
CREATE TABLE told_briefings (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    day TEXT NOT NULL UNIQUE REFERENCES briefings (day),
    act TEXT NOT NULL,
    version INTEGER NOT NULL,
    model TEXT NOT NULL,
    request_hash TEXT NOT NULL
) STRICT;

-- A statement of the Briefing, at its place in it, in the Editor's words.
CREATE TABLE told_statements (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    briefing_id INTEGER NOT NULL REFERENCES told_briefings (event_id),
    position INTEGER NOT NULL,
    text TEXT NOT NULL CHECK (text != ''),
    UNIQUE (briefing_id, position)
) STRICT;

-- The Spans a statement cites, in order, each with the reading it was shown
-- as (NULL for a Span of an Item only cut).
CREATE TABLE told_citations (
    statement_id INTEGER NOT NULL REFERENCES told_statements (event_id),
    position INTEGER NOT NULL,
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    reading_id INTEGER REFERENCES readings (event_id),
    PRIMARY KEY (statement_id, position),
    CHECK (0 <= span_start AND span_start < span_end),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX told_citations_by_span ON told_citations (source_id, entry_id, span_start, span_end);

-- A Story the Briefing gathered from a Span it told: the Spans its
-- relations reach that move on what it says, or that it moves on.
CREATE TABLE gatherings (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    briefing_id INTEGER NOT NULL REFERENCES told_briefings (event_id),
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    CHECK (0 <= span_start AND span_start < span_end),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX gatherings_by_span ON gatherings (source_id, entry_id, span_start, span_end, event_id);

CREATE TABLE gathered_spans (
    gathering_id INTEGER NOT NULL REFERENCES gatherings (event_id),
    position INTEGER NOT NULL,
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    PRIMARY KEY (gathering_id, position),
    CHECK (0 <= span_start AND span_start < span_end),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE TRIGGER told_briefings_not_rewritten BEFORE UPDATE ON told_briefings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER told_briefings_not_removed BEFORE DELETE ON told_briefings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER told_statements_not_rewritten BEFORE UPDATE ON told_statements
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER told_statements_not_removed BEFORE DELETE ON told_statements
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER told_citations_not_rewritten BEFORE UPDATE ON told_citations
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER told_citations_not_removed BEFORE DELETE ON told_citations
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER gatherings_not_rewritten BEFORE UPDATE ON gatherings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER gatherings_not_removed BEFORE DELETE ON gatherings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER gathered_spans_not_rewritten BEFORE UPDATE ON gathered_spans
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER gathered_spans_not_removed BEFORE DELETE ON gathered_spans
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
