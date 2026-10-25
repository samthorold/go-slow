-- What the register reads that the rest of the Record does not hold: the
-- outputs the Owner opened, and the commits to go-slow, for the quiet
-- window and for changes to the Editor.

-- An output opened: a Briefing or a Sunday read (by its day), on a day. An
-- output opened many times on one day is kept once.
CREATE TABLE opens (
    output TEXT NOT NULL CHECK (output IN ('briefing', 'sunday read')),
    day TEXT NOT NULL, -- the output's, YYYY-MM-DD
    opened_on TEXT NOT NULL, -- YYYY-MM-DD, in the Owner's time zone
    PRIMARY KEY (output, day, opened_on)
) STRICT;

-- A commit to go-slow, recorded from git by the commits admin command.
CREATE TABLE commits (
    hash TEXT PRIMARY KEY,
    committed_at TEXT NOT NULL,
    editor INTEGER NOT NULL CHECK (editor IN (0, 1)) -- changes the Editor
) STRICT;
