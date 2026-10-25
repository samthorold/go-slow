-- Entities from Wikidata first: an Entity may be a Wikidata item, and keeps
-- every name the Record's Items have called it by. An Entity the Editor made
-- that later matches a Wikidata item becomes that item, its name kept as an
-- alias, so the Record's history still resolves.

-- The Wikidata item an Entity is; NULL for the Editor's own.
ALTER TABLE entities ADD COLUMN wikidata_id TEXT;
CREATE UNIQUE INDEX entities_wikidata_id ON entities (wikidata_id) WHERE wikidata_id IS NOT NULL;

-- The names an Entity is called by.
CREATE TABLE entity_names (
    entity_id INTEGER NOT NULL REFERENCES entities (id),
    name TEXT NOT NULL,
    PRIMARY KEY (entity_id, name)
) STRICT;
CREATE INDEX entity_names_name ON entity_names (name);

INSERT INTO entity_names (entity_id, name) SELECT id, name FROM entities;

-- A consultation of a Reference source: for Wikidata, the item and the
-- revision of it seen. Wikidata's structured data is CC0.
CREATE TABLE reference_lookups (
    id INTEGER PRIMARY KEY,
    reference TEXT NOT NULL, -- the Reference source, such as Wikidata
    item_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    looked_up_at TEXT NOT NULL,
    licence TEXT NOT NULL
) STRICT;

-- A Fact drawn from a Reference source, traced to the lookup it came from.
-- The same Fact found by a later lookup is kept once, traced to the first.
CREATE TABLE reference_facts (
    id INTEGER PRIMARY KEY,
    lookup_id INTEGER NOT NULL REFERENCES reference_lookups (id),
    statement TEXT NOT NULL,
    as_at TEXT NOT NULL, -- YYYY-MM-DD
    quote TEXT NOT NULL, -- the Reference source's statement it rests on
    UNIQUE (statement, as_at)
) STRICT;

CREATE TABLE reference_fact_entities (
    fact_id INTEGER NOT NULL REFERENCES reference_facts (id),
    entity_id INTEGER NOT NULL REFERENCES entities (id),
    PRIMARY KEY (fact_id, entity_id)
) STRICT;
