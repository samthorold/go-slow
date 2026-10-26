-- The role affected is renamed undergoer: what or whom a Fact's happening
-- was done to, or what the Fact says something of, with no judgement of harm.
-- SQLite cannot change a CHECK in place, so fact_entities is rebuilt with
-- the new set of roles and the roles kept are renamed as they are copied.
-- No table refers to fact_entities, so nothing else is touched.
ALTER TABLE fact_entities RENAME TO fact_entities_0019;
CREATE TABLE fact_entities (
    fact_id INTEGER NOT NULL REFERENCES facts (id),
    entity_id INTEGER NOT NULL REFERENCES entities (id),
    -- The role an Entity has in a Fact, once connecting has given it one.
    role TEXT CHECK (role IN ('actor', 'undergoer', 'place', 'occasion')),
    PRIMARY KEY (fact_id, entity_id)
) STRICT;
INSERT INTO fact_entities (fact_id, entity_id, role)
    SELECT fact_id, entity_id, CASE role WHEN 'affected' THEN 'undergoer' ELSE role END
    FROM fact_entities_0019;
DROP TABLE fact_entities_0019;
