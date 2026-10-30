-- What the Spans name, looked up in Wikidata by plain code, as events
-- (system design: Entities are looked up by name, not by Span; Entities
-- come from Wikidata first, otherwise from the Editor).
--
-- A look-up (kind 'lookup') is the judgement of what a key names: the name,
-- its kind, and for a person named with an office, the office and the
-- Item's date (on, 2006-01-02 in UTC; '' with no office). Its judge is the
-- look-up code, by name and version. It matched a Wikidata item (its id,
-- English label and the revision seen), or nothing (wikidata_id ''). A
-- look-up Wikidata refused is no judgement, and is kept nowhere.
CREATE TABLE lookups (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('person', 'organisation', 'place', 'occasion')),
    office TEXT NOT NULL,
    on_date TEXT NOT NULL,
    looker TEXT NOT NULL,
    looker_version INTEGER NOT NULL,
    wikidata_id TEXT NOT NULL,
    label TEXT NOT NULL,
    revision INTEGER NOT NULL,
    CHECK (office = '' OR kind = 'person'),
    CHECK (office != '' OR on_date = '')
) STRICT;

CREATE INDEX lookups_by_key ON lookups (name, kind, office, on_date, event_id);

-- An Entity the Editor made (kind 'entity'), for a name and kind a look-up
-- matched to nothing: one per name and kind.
CREATE TABLE editor_entities (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('person', 'organisation', 'place', 'occasion')),
    lookup_id INTEGER NOT NULL REFERENCES lookups (event_id)
) STRICT;

CREATE INDEX editor_entities_by_name ON editor_entities (name, kind);

-- An Editor's Entity merged into a Wikidata item (kind 'merge'), by a later
-- look-up that matched the item under the Entity's name or the item's
-- label. The Editor's Entity stays, as an alias of the item.
CREATE TABLE entity_merges (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    entity_id INTEGER NOT NULL REFERENCES editor_entities (event_id),
    lookup_id INTEGER NOT NULL REFERENCES lookups (event_id)
) STRICT;

CREATE INDEX entity_merges_by_entity ON entity_merges (entity_id, event_id);

CREATE TRIGGER lookups_not_rewritten BEFORE UPDATE ON lookups
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER lookups_not_removed BEFORE DELETE ON lookups
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER editor_entities_not_rewritten BEFORE UPDATE ON editor_entities
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER editor_entities_not_removed BEFORE DELETE ON editor_entities
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER entity_merges_not_rewritten BEFORE UPDATE ON entity_merges
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER entity_merges_not_removed BEFORE DELETE ON entity_merges
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
