-- What writing the Briefing made: for each day, the Stories it told, in
-- order, each in statements citing the Developments they rest on. A
-- Briefing is written once and never rewritten.
ALTER TABLE briefings ADD COLUMN written_at TEXT NOT NULL DEFAULT '';
ALTER TABLE briefings ADD COLUMN model TEXT NOT NULL DEFAULT '';

-- A Story the day's Briefing tells, at its place in the Briefing.
CREATE TABLE briefing_stories (
    day TEXT NOT NULL REFERENCES briefings (day),
    position INTEGER NOT NULL,
    story_id INTEGER NOT NULL REFERENCES stories (id),
    PRIMARY KEY (day, position),
    UNIQUE (day, story_id)
) STRICT;

-- A statement in the Briefing, about one of its Stories.
CREATE TABLE briefing_statements (
    id INTEGER PRIMARY KEY,
    day TEXT NOT NULL,
    story_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    text TEXT NOT NULL CHECK (text != ''),
    FOREIGN KEY (day, story_id) REFERENCES briefing_stories (day, story_id)
) STRICT;

-- The Developments a statement cites: each traces to its Fact and Item.
CREATE TABLE statement_developments (
    statement_id INTEGER NOT NULL REFERENCES briefing_statements (id),
    story_id INTEGER NOT NULL,
    fact_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    PRIMARY KEY (statement_id, fact_id),
    FOREIGN KEY (story_id, fact_id) REFERENCES developments (story_id, fact_id)
) STRICT;

-- A Briefing that failed to be written (a failed call, or a result that
-- failed the act's checks), kept so the failure is shown.
CREATE TABLE briefing_failures (
    id INTEGER PRIMARY KEY,
    day TEXT NOT NULL, -- YYYY-MM-DD, in the Owner's time zone
    failed_at TEXT NOT NULL,
    reason TEXT NOT NULL
) STRICT;
