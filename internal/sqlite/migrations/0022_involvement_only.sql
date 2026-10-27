-- A Fact is tied to an Entity only by a role connecting gives it: which
-- Entities a Fact involves is connecting's judgement, and what finds work
-- reads the Item's mentions (item_mentions), which stay as they are. The
-- ties readings made with no role (each Fact to every Entity its Item
-- mentions, or, from earlier readings, to the list given with it) go, and
-- fact_entities is rebuilt with its role required, by SQLite's procedure
-- (create the new table, copy, drop the old, rename). No table refers to
-- fact_entities. Claims were tied only by earlier readings' lists, already
-- kept as their Items' mentions, so claim_entities goes.
CREATE TABLE new_fact_entities (
    fact_id INTEGER NOT NULL REFERENCES facts (id),
    entity_id INTEGER NOT NULL REFERENCES entities (id),
    -- The role connecting gave the Entity in the Fact.
    role TEXT NOT NULL CHECK (role IN ('actor', 'undergoer', 'place', 'occasion')),
    PRIMARY KEY (fact_id, entity_id)
) STRICT;
INSERT INTO new_fact_entities (fact_id, entity_id, role)
    SELECT fact_id, entity_id, role FROM fact_entities WHERE role IS NOT NULL;
DROP TABLE fact_entities;
ALTER TABLE new_fact_entities RENAME TO fact_entities;
DROP TABLE claim_entities;
