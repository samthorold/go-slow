-- The Sources Collection checks, with what each feed's server said last time
-- (sent back to make the next request conditional).
CREATE TABLE sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    feed_url TEXT NOT NULL,
    usual_gap_seconds INTEGER NOT NULL,
    added_at TEXT NOT NULL, -- when Collection began checking it

    etag TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT ''
) STRICT;

-- Every Item Collection brought in, kept in full: never updated or deleted.
CREATE TABLE items (
    source_id TEXT NOT NULL REFERENCES sources (id),
    entry_id TEXT NOT NULL, -- the feed's id for the entry
    title TEXT NOT NULL,
    link TEXT NOT NULL,
    summary TEXT NOT NULL,
    content TEXT NOT NULL,
    published TEXT NOT NULL, -- RFC 3339, UTC; '' if the feed gave none
    updated TEXT NOT NULL,
    raw TEXT NOT NULL, -- the entry's XML as the feed carried it
    attribution TEXT NOT NULL,
    collected_at TEXT NOT NULL,
    PRIMARY KEY (source_id, entry_id)
) STRICT;
