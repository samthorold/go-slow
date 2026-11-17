-- The Owner reads Stories, and the old design's acts are retired (system
-- design: The Record is recreated when the design of its Judgements changes,
-- not translated): relating, the Briefing, the Sunday read, the gatherings
-- they made and the Feedback given in the app. What they wrote stays in the
-- Record as events no projection reads, in their tables (relatings,
-- relations, briefings, told_briefings, told_statements, told_citations,
-- gatherings, gathered_spans, sunday_reads, told_reads, told_assessments,
-- assessment_grounds, told_read_statements, told_read_citations,
-- feedback_given, feedback_undoings, judgement_precedents), as do their
-- failures (briefing_failures, sunday_read_failures), the register's opens
-- and commits, and every kept call and conversation. Nothing is dropped or
-- rewritten.

-- The switch to Stories (kind 'switch', shape 2), kept when the Record held
-- Cuts: the Stories cut through through_cut are woven only on the Owner's
-- say, never as work awaiting weaving, so the first start after it does not
-- show weaving the whole Record. A Record already woven carries on from its
-- last run's newest Cut, so the switch takes nothing from it. A Record with
-- nothing cut has no switch: all of it is the new design's. (The first
-- switch, kind 'switch' shape 1 in record_switch, still bounds reading.)
CREATE TABLE story_switch (
    event_id INTEGER PRIMARY KEY REFERENCES events (id),
    through_cut INTEGER NOT NULL REFERENCES cuts (event_id)
) STRICT;

INSERT INTO events (kind, shape, at)
SELECT 'switch', 2, strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z'
WHERE EXISTS (SELECT 1 FROM cuts);

INSERT INTO story_switch (event_id, through_cut)
SELECT last_insert_rowid(),
    coalesce((SELECT max(through_cut) FROM weavings), (SELECT max(event_id) FROM cuts))
WHERE EXISTS (SELECT 1 FROM cuts);

CREATE TRIGGER story_switch_not_rewritten BEFORE UPDATE ON story_switch
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER story_switch_not_removed BEFORE DELETE ON story_switch
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

-- Work on the acts the Editor no longer runs: it awaits nothing now, so its
-- failures are no longer listed or retried. Their failed calls stay kept.
DELETE FROM work_attempts WHERE act IN ('relating', 'briefing', 'sunday_read');
