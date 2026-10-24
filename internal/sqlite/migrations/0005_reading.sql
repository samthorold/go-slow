-- What reading an Item drew from it: Facts and Claims, each tied to the
-- Entities it involves. An Item is read once; a reading is never rewritten.

-- The Items read, when, and by which model.
CREATE TABLE item_reads (
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    read_at TEXT NOT NULL,
    model TEXT NOT NULL,
    PRIMARY KEY (source_id, entry_id),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

-- go-slow's own Entities, made by reading, one per name.
CREATE TABLE entities (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL -- person, organisation, place or occasion
) STRICT;

-- A Fact, drawn from an Item and holding as at a date.
CREATE TABLE facts (
    id INTEGER PRIMARY KEY,
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    statement TEXT NOT NULL,
    as_at TEXT NOT NULL, -- YYYY-MM-DD
    quote TEXT NOT NULL, -- the Item's words it rests on
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE TABLE fact_entities (
    fact_id INTEGER NOT NULL REFERENCES facts (id),
    entity_id INTEGER NOT NULL REFERENCES entities (id),
    PRIMARY KEY (fact_id, entity_id)
) STRICT;

-- A Claim, found in an Item and attributed to whoever made it.
CREATE TABLE claims (
    id INTEGER PRIMARY KEY,
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    statement TEXT NOT NULL,
    made_by TEXT NOT NULL CHECK (made_by != ''),
    quote TEXT NOT NULL,
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;

CREATE TABLE claim_entities (
    claim_id INTEGER NOT NULL REFERENCES claims (id),
    entity_id INTEGER NOT NULL REFERENCES entities (id),
    PRIMARY KEY (claim_id, entity_id)
) STRICT;
