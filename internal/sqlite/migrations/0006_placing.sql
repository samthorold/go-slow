-- What placing a Fact made of it: Stories, and the Facts that are
-- Developments in them. Stories never close.

-- A Story: an ongoing subject, followed across days.
CREATE TABLE stories (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL CHECK (title != ''),
    started_at TEXT NOT NULL
) STRICT;

-- A Development: a Fact that moves a Story on. A Fact can be one in several
-- Stories.
CREATE TABLE developments (
    story_id INTEGER NOT NULL REFERENCES stories (id),
    fact_id INTEGER NOT NULL REFERENCES facts (id),
    PRIMARY KEY (story_id, fact_id)
) STRICT;

CREATE INDEX developments_by_fact ON developments (fact_id);

-- The Facts placed, when, and by which model, including those placed in no
-- Story.
CREATE TABLE fact_placings (
    fact_id INTEGER PRIMARY KEY REFERENCES facts (id),
    placed_at TEXT NOT NULL,
    model TEXT NOT NULL
) STRICT;
