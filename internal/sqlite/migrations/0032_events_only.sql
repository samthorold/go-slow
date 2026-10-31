-- The Record keeps only its Items and its events (system design: The Record
-- as events). None of the old design's judgements are carried: its Facts,
-- Claims, Stories, Developments, Connections, Briefings and Sunday reads over
-- them, their Assessments and the Feedback given on them, and the marks and
-- ties they made, go with their tables. What stays: Sources and Items with
-- their text and collection times; Topics, without the Depth and Sources
-- that selected Items; every event kept since the Record became events; the
-- kept calls and conversations, which are never removed; and what the
-- register reads besides (the days an output was written or failed, opens
-- and commits).

-- The switch (kind 'switch'), kept when the Record held Items: Items
-- collected through the newest of them were collected under the old design,
-- and are read only by re-judging them, never as work awaiting reading, so
-- the first start after it does not submit the whole Record. A Record that
-- held no Items has no switch: all of it is the new design's.
CREATE TABLE record_switch (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    items_through TEXT NOT NULL -- the newest Item's collected_at
) STRICT;

INSERT INTO events (kind, shape, at)
SELECT 'switch', 1, strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z'
WHERE EXISTS (SELECT 1 FROM items);

INSERT INTO record_switch (event_id, items_through)
SELECT last_insert_rowid(), (SELECT max(collected_at) FROM items)
WHERE EXISTS (SELECT 1 FROM items);

CREATE TRIGGER record_switch_not_rewritten BEFORE UPDATE ON record_switch
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER record_switch_not_removed BEFORE DELETE ON record_switch
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

-- The Briefings and Sunday reads written before they told Spans keep only
-- their days, in briefings and sunday_reads: that an output was written on
-- a day is what the register reads (The outputs appear, and the windows the
-- opens are read over), and their content goes with its tables below.

-- Work on the acts the Editor no longer runs.
DELETE FROM work_attempts WHERE act IN ('placing', 'connecting');

DROP TABLE read_citations;
DROP TABLE read_stories;
DROP TABLE read_statements;
DROP TABLE assessment_facts;
DROP TABLE assessments;
DROP TABLE statement_developments;
DROP TABLE briefing_statements;
DROP TABLE briefing_stories;
DROP TABLE developments;
DROP TABLE stories;
DROP TABLE fact_connections;
DROP TABLE fact_connectings;
DROP TABLE fact_placings;
DROP TABLE fact_entities;
DROP TABLE facts;
DROP TABLE claims;
DROP TABLE precedents_shown;
DROP TABLE feedback;
DROP TABLE reference_fact_entities;
DROP TABLE reference_facts;
DROP TABLE item_mentions;
DROP TABLE entity_names;
DROP TABLE entities;
DROP TABLE item_topics;
DROP TABLE item_checks;
DROP TABLE item_reads;

ALTER TABLE topics DROP COLUMN depth;
ALTER TABLE topics DROP COLUMN sources;
