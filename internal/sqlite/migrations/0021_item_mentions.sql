-- What an Item mentions: every Entity it names, each once. Reading gives an
-- Item's mentions once, apart from its Facts and Claims; earlier readings
-- gave the Entities each Fact and Claim involved, and together those lists
-- are what their Item mentions, so they are kept as its mentions here. The
-- lists themselves stay as they were, tying each statement to its Entities.
CREATE TABLE item_mentions (
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    entity_id INTEGER NOT NULL REFERENCES entities (id),
    PRIMARY KEY (source_id, entry_id, entity_id),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;
CREATE INDEX item_mentions_entity ON item_mentions (entity_id);

INSERT INTO item_mentions (source_id, entry_id, entity_id)
    SELECT f.source_id, f.entry_id, fe.entity_id FROM fact_entities fe JOIN facts f ON f.id = fe.fact_id
    UNION
    SELECT c.source_id, c.entry_id, ce.entity_id FROM claim_entities ce JOIN claims c ON c.id = ce.claim_id;
