-- The Owner's Feedback, kept as given: what was said, about what, at what
-- Depth and when. It holds no effect, so a changed mechanism can be run
-- again over all of it; its effects are marks on the Record that name the
-- Feedback that made them. Nothing is deleted by Feedback.
CREATE TABLE feedback (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('missed', 'unwanted', 'wrong', 'new to me')),
    level TEXT NOT NULL CHECK (level IN ('story', 'development', 'fact', 'connection')),
    story_id INTEGER, -- the Story, or a Development's; none for a missed Story
    fact_id INTEGER, -- the Fact, a Development's, or a Connection's cause
    to_fact_id INTEGER, -- a Connection's effect
    title TEXT NOT NULL DEFAULT '', -- a missed Story's, as typed
    fact_ids TEXT NOT NULL DEFAULT '[]', -- the Facts a missed Story gathers
    day TEXT NOT NULL DEFAULT '', -- the Briefing it was given on, if any
    depth INTEGER NOT NULL, -- 0 when no Topic ties what it is about
    given_at TEXT NOT NULL
) STRICT;

-- A Story the Owner said was missed, and one they did not want in the
-- Briefing.
ALTER TABLE stories ADD COLUMN missed_by INTEGER REFERENCES feedback (id);
ALTER TABLE stories ADD COLUMN unwanted_by INTEGER REFERENCES feedback (id);

-- Likewise a Development, and one withdrawn with its wrong Fact.
ALTER TABLE developments ADD COLUMN missed_by INTEGER REFERENCES feedback (id);
ALTER TABLE developments ADD COLUMN unwanted_by INTEGER REFERENCES feedback (id);
ALTER TABLE developments ADD COLUMN withdrawn_by INTEGER REFERENCES feedback (id);

-- A Fact the Owner said was wrong.
ALTER TABLE facts ADD COLUMN withdrawn_by INTEGER REFERENCES feedback (id);

-- A Connection the Owner said was wrong, or withdrawn with a wrong Fact it
-- connects.
ALTER TABLE fact_connections ADD COLUMN withdrawn_by INTEGER REFERENCES feedback (id);
