-- The Sunday read over Spans, as events (system design: The Sunday read
-- writes Assessments and tells wider stories; it keeps no Story; The read's
-- citations are refs to what its agent was shown). A read (kind
-- 'sunday_read') is the Editor's judgement for a Sunday, with its Judge: the
-- act, its instructions' version and the model that answered (empty for one
-- written with no call). Its day, and the run's turns and tokens, are in
-- sunday_reads, which keeps every Sunday a read was written for, in either
-- shape. Each statement it makes (kind 'read_statement'), each Assessment
-- it reaches (kind 'assessment') and each Story it gathers (kind
-- 'gathering', in gatherings) is an event of its own, tied to it, so
-- Feedback can land on each. Every address is a Span: byte offsets into its
-- Item's text as kept, start inclusive and end exclusive. The tables the
-- read kept before it told Spans (assessments, read_statements,
-- read_citations, read_stories) are read as they were.
CREATE TABLE told_reads (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    day TEXT NOT NULL UNIQUE REFERENCES sunday_reads (day),
    act TEXT NOT NULL,
    version INTEGER NOT NULL,
    model TEXT NOT NULL
) STRICT;

-- An Assessment: a judgement the read reached, as at its Sunday, never
-- rewritten; a later one supersedes it.
CREATE TABLE told_assessments (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    read_id INTEGER NOT NULL REFERENCES told_reads (event_id),
    position INTEGER NOT NULL,
    statement TEXT NOT NULL CHECK (statement != ''),
    as_at TEXT NOT NULL, -- YYYY-MM-DD
    supersedes_id INTEGER UNIQUE REFERENCES told_assessments (event_id),
    UNIQUE (read_id, position)
) STRICT;

-- What an Assessment rests on, in order: a Span, with the reading it was
-- shown as (NULL for a Span of an Item only cut), or an earlier Assessment.
CREATE TABLE assessment_grounds (
    assessment_id INTEGER NOT NULL REFERENCES told_assessments (event_id),
    position INTEGER NOT NULL,
    source_id TEXT,
    entry_id TEXT,
    span_start INTEGER,
    span_end INTEGER,
    reading_id INTEGER REFERENCES readings (event_id),
    earlier_id INTEGER REFERENCES told_assessments (event_id),
    PRIMARY KEY (assessment_id, position),
    CHECK ((earlier_id IS NULL) = (source_id IS NOT NULL AND entry_id IS NOT NULL
        AND span_start IS NOT NULL AND span_end IS NOT NULL)),
    CHECK (span_start IS NULL OR (0 <= span_start AND span_start < span_end)),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

-- A statement of the read, at its place in it, in the Editor's words.
CREATE TABLE told_read_statements (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    read_id INTEGER NOT NULL REFERENCES told_reads (event_id),
    position INTEGER NOT NULL,
    text TEXT NOT NULL CHECK (text != ''),
    UNIQUE (read_id, position)
) STRICT;

-- What a statement cites, in order: exactly one of a Span (with the reading
-- it was shown as), an Assessment, or a lookup of a Reference source.
CREATE TABLE told_read_citations (
    statement_id INTEGER NOT NULL REFERENCES told_read_statements (event_id),
    position INTEGER NOT NULL,
    source_id TEXT,
    entry_id TEXT,
    span_start INTEGER,
    span_end INTEGER,
    reading_id INTEGER REFERENCES readings (event_id),
    assessment_id INTEGER REFERENCES told_assessments (event_id),
    lookup_id INTEGER REFERENCES reference_lookups (id),
    PRIMARY KEY (statement_id, position),
    CHECK ((source_id IS NOT NULL AND entry_id IS NOT NULL AND span_start IS NOT NULL AND span_end IS NOT NULL)
        + (assessment_id IS NOT NULL) + (lookup_id IS NOT NULL) = 1),
    CHECK (span_start IS NULL OR (0 <= span_start AND span_start < span_end)),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE INDEX told_read_citations_by_span ON told_read_citations (source_id, entry_id, span_start, span_end);

-- A Story is gathered by the pass that tells it, the Briefing or the Sunday
-- read: gatherings is rebuilt so a gathering names either, by SQLite's own
-- procedure (create new_gatherings, copy, drop gatherings, rename), since a
-- constraint can't change in place. Dropping it drops its triggers and
-- index, made again below.
CREATE TABLE new_gatherings (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    briefing_id INTEGER REFERENCES told_briefings (event_id),
    read_id INTEGER REFERENCES told_reads (event_id),
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    span_start INTEGER NOT NULL,
    span_end INTEGER NOT NULL,
    CHECK ((briefing_id IS NULL) != (read_id IS NULL)),
    CHECK (0 <= span_start AND span_start < span_end),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

INSERT INTO new_gatherings (event_id, briefing_id, source_id, entry_id, span_start, span_end)
    SELECT event_id, briefing_id, source_id, entry_id, span_start, span_end FROM gatherings;
DROP TABLE gatherings;
ALTER TABLE new_gatherings RENAME TO gatherings;

CREATE INDEX gatherings_by_span ON gatherings (source_id, entry_id, span_start, span_end, event_id);

CREATE TRIGGER gatherings_not_rewritten BEFORE UPDATE ON gatherings
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER gatherings_not_removed BEFORE DELETE ON gatherings
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER told_reads_not_rewritten BEFORE UPDATE ON told_reads
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER told_reads_not_removed BEFORE DELETE ON told_reads
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER told_assessments_not_rewritten BEFORE UPDATE ON told_assessments
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER told_assessments_not_removed BEFORE DELETE ON told_assessments
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER assessment_grounds_not_rewritten BEFORE UPDATE ON assessment_grounds
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER assessment_grounds_not_removed BEFORE DELETE ON assessment_grounds
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER told_read_statements_not_rewritten BEFORE UPDATE ON told_read_statements
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER told_read_statements_not_removed BEFORE DELETE ON told_read_statements
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER told_read_citations_not_rewritten BEFORE UPDATE ON told_read_citations
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER told_read_citations_not_removed BEFORE DELETE ON told_read_citations
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
