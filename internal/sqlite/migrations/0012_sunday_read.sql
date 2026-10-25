-- What the Sunday read made: for each Sunday, its statements, each citing
-- what it rests on (a Fact, a Development, an Assessment or a lookup of a
-- Reference source); the Assessments it reached, citing their Facts; and the
-- Stories it started, gathering existing Developments. A read is written
-- once and never rewritten; an Assessment is never rewritten either, and a
-- later one supersedes it.

CREATE TABLE sunday_reads (
    id INTEGER PRIMARY KEY,
    day TEXT NOT NULL UNIQUE, -- YYYY-MM-DD, in the Owner's time zone
    written_at TEXT NOT NULL,
    model TEXT NOT NULL,
    turns INTEGER NOT NULL,
    input_tokens INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL
) STRICT;

-- An Assessment: a judgement the Editor reached, as at a date.
CREATE TABLE assessments (
    id INTEGER PRIMARY KEY,
    read_id INTEGER NOT NULL REFERENCES sunday_reads (id),
    position INTEGER NOT NULL,
    statement TEXT NOT NULL CHECK (statement != ''),
    as_at TEXT NOT NULL, -- YYYY-MM-DD
    -- The earlier Assessment this one supersedes, which is kept as it was.
    supersedes_id INTEGER UNIQUE REFERENCES assessments (id),
    -- The Feedback that said it was wrong. Like a Fact, it is marked, never
    -- deleted, and the statements that cite it are shown no more.
    withdrawn_by INTEGER REFERENCES feedback (id),
    UNIQUE (read_id, position)
) STRICT;

-- The Facts an Assessment rests on.
CREATE TABLE assessment_facts (
    assessment_id INTEGER NOT NULL REFERENCES assessments (id),
    fact_id INTEGER NOT NULL REFERENCES facts (id),
    position INTEGER NOT NULL,
    PRIMARY KEY (assessment_id, fact_id)
) STRICT;

-- A statement in the Sunday read.
CREATE TABLE read_statements (
    id INTEGER PRIMARY KEY,
    read_id INTEGER NOT NULL REFERENCES sunday_reads (id),
    position INTEGER NOT NULL,
    text TEXT NOT NULL CHECK (text != ''),
    UNIQUE (read_id, position)
) STRICT;

-- What a statement cites: exactly one thing of its kind.
CREATE TABLE read_citations (
    statement_id INTEGER NOT NULL REFERENCES read_statements (id),
    position INTEGER NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('fact', 'development', 'assessment', 'lookup')),
    fact_id INTEGER REFERENCES facts (id),
    story_id INTEGER REFERENCES stories (id),
    assessment_id INTEGER REFERENCES assessments (id),
    lookup_id INTEGER REFERENCES reference_lookups (id),
    PRIMARY KEY (statement_id, position),
    FOREIGN KEY (story_id, fact_id) REFERENCES developments (story_id, fact_id),
    CHECK (
        (kind = 'fact' AND fact_id IS NOT NULL AND story_id IS NULL AND assessment_id IS NULL AND lookup_id IS NULL)
        OR (kind = 'development' AND fact_id IS NOT NULL AND story_id IS NOT NULL AND assessment_id IS NULL AND lookup_id IS NULL)
        OR (kind = 'assessment' AND assessment_id IS NOT NULL AND fact_id IS NULL AND story_id IS NULL AND lookup_id IS NULL)
        OR (kind = 'lookup' AND lookup_id IS NOT NULL AND fact_id IS NULL AND story_id IS NULL AND assessment_id IS NULL)
    )
) STRICT;

-- The Stories a Sunday read started.
CREATE TABLE read_stories (
    read_id INTEGER NOT NULL REFERENCES sunday_reads (id),
    story_id INTEGER NOT NULL UNIQUE REFERENCES stories (id),
    PRIMARY KEY (read_id, story_id)
) STRICT;

-- Feedback on a Sunday read and its Assessments: the feedback table is
-- rebuilt with the two levels and what each is about, since SQLite cannot
-- change a CHECK in place. With legacy_alter_table on, renaming the old
-- table leaves the references to feedback from other tables naming
-- feedback, which the new table then is; the rows keep their ids.
PRAGMA legacy_alter_table = ON;
ALTER TABLE feedback RENAME TO feedback_0011;
CREATE TABLE feedback (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('missed', 'unwanted', 'wrong', 'new to me')),
    level TEXT NOT NULL CHECK (level IN ('story', 'development', 'fact', 'connection', 'assessment', 'sunday read')),
    story_id INTEGER, -- the Story, or a Development's; none for a missed Story
    fact_id INTEGER, -- the Fact, a Development's, or a Connection's cause
    to_fact_id INTEGER, -- a Connection's effect
    title TEXT NOT NULL DEFAULT '', -- a missed Story's, as typed
    fact_ids TEXT NOT NULL DEFAULT '[]', -- the Facts a missed Story gathers
    day TEXT NOT NULL DEFAULT '', -- the Briefing or Sunday read it was given on, if any
    depth INTEGER NOT NULL, -- 0 when no Topic ties what it is about
    given_at TEXT NOT NULL,
    assessment_id INTEGER, -- the Assessment
    read_id INTEGER -- the Sunday read
) STRICT;
INSERT INTO feedback (id, kind, level, story_id, fact_id, to_fact_id, title, fact_ids, day, depth, given_at)
    SELECT id, kind, level, story_id, fact_id, to_fact_id, title, fact_ids, day, depth, given_at FROM feedback_0011;
DROP TABLE feedback_0011;
PRAGMA legacy_alter_table = OFF;

-- A Sunday read that failed to be written, kept so the failure is shown and
-- the tries on a Sunday are counted.
CREATE TABLE sunday_read_failures (
    id INTEGER PRIMARY KEY,
    day TEXT NOT NULL,
    failed_at TEXT NOT NULL,
    reason TEXT NOT NULL
) STRICT;
