-- What connecting a Fact made of it: the role each Entity it involves has
-- in it, and cause Connections between Facts. Connections are made only
-- between Facts, and from a Fact to its Entities; Connections between
-- Stories are derived from shared Developments and shared Entities in the
-- same roles, never stored.

-- The role an Entity has in a Fact, once connecting has given it one.
ALTER TABLE fact_entities ADD COLUMN role TEXT
    CHECK (role IN ('actor', 'affected', 'place', 'occasion'));

-- A Connection between two Facts, of a kind: one Fact caused another.
CREATE TABLE fact_connections (
    from_fact_id INTEGER NOT NULL REFERENCES facts (id),
    to_fact_id INTEGER NOT NULL REFERENCES facts (id),
    kind TEXT NOT NULL CHECK (kind IN ('cause')),
    made_at TEXT NOT NULL,
    model TEXT NOT NULL,
    PRIMARY KEY (from_fact_id, to_fact_id, kind),
    CHECK (from_fact_id != to_fact_id)
) STRICT;

CREATE INDEX fact_connections_to ON fact_connections (to_fact_id);

-- The Facts connected, when last, and by which model.
CREATE TABLE fact_connectings (
    fact_id INTEGER PRIMARY KEY REFERENCES facts (id),
    connected_at TEXT NOT NULL,
    model TEXT NOT NULL
) STRICT;
