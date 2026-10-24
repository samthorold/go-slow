-- The Owner's Topics, each at a Depth, with what the lazy-reading check ties
-- Items to them by.
CREATE TABLE topics (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    depth INTEGER NOT NULL, -- 1 passing, 2 following, 3 close
    sources TEXT NOT NULL, -- Source ids, one per line
    terms TEXT NOT NULL -- keywords and names, one per line
) STRICT;

-- The Items the lazy-reading check has looked at, whether or not it tied
-- them to a Topic.
CREATE TABLE item_checks (
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    PRIMARY KEY (source_id, entry_id),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

-- An Item awaiting reading for a Topic: the check tied it to the Topic.
CREATE TABLE item_topics (
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    topic_id INTEGER NOT NULL REFERENCES topics (id) ON DELETE CASCADE,
    PRIMARY KEY (source_id, entry_id, topic_id),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;
