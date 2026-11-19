-- The act that judges the Spans of the Story a Cut made is called
-- hydration, as the design calls it, wherever the Record keeps its name (the
-- Owner, 2026-11-19: the Record need not stay backwards compatible). Its
-- judgement events are of kind 'hydration' and its tables say hydration
-- (hydrations, candidate_hydrations, each judgement's hydration_id); its
-- Judges, kept calls, kept conversations, work attempts and failed marks and
-- pending batches name the act 'hydration'. Nothing reads the old name
-- afterwards. The request a kept call answered is unchanged: its hash covers
-- the instructions, input, schema, model and effort, never the act's name.
--
-- This is the one rewrite of events and kept calls: the triggers refusing it
-- are dropped for it and made again as they were. The retired acts' tables
-- (relatings, told_citations, assessment_grounds) keep their reading_id
-- columns, now referring to hydrations.

DROP TRIGGER events_not_rewritten;
DROP TRIGGER readings_not_rewritten;
DROP TRIGGER readings_not_removed;
DROP TRIGGER candidates_not_rewritten;
DROP TRIGGER candidate_readings_not_rewritten;
DROP TRIGGER candidate_readings_not_removed;
DROP TRIGGER model_calls_not_rewritten;
DROP TRIGGER conversations_not_rewritten;

UPDATE events SET kind = 'hydration' WHERE kind = 'reading';
UPDATE readings SET act = 'hydration' WHERE act = 'reading';
UPDATE candidates SET act = 'hydration' WHERE act = 'reading';
UPDATE model_calls SET act = 'hydration' WHERE act = 'reading';
UPDATE conversations SET act = 'hydration' WHERE act = 'reading';
UPDATE work_attempts SET act = 'hydration' WHERE act = 'reading';
UPDATE batch_requests SET act = 'hydration' WHERE act = 'reading';

DROP INDEX readings_by_item;
DROP INDEX refers_to_by_reading;
DROP INDEX span_names_by_reading;
DROP INDEX speakers_by_reading;
DROP INDEX rejections_by_reading;
DROP INDEX candidate_readings_by_candidate;

ALTER TABLE readings RENAME TO hydrations;
ALTER TABLE candidate_readings RENAME TO candidate_hydrations;
ALTER TABLE candidate_hydrations RENAME COLUMN reading_id TO hydration_id;
ALTER TABLE refers_to RENAME COLUMN reading_id TO hydration_id;
ALTER TABLE span_names RENAME COLUMN reading_id TO hydration_id;
ALTER TABLE speakers RENAME COLUMN reading_id TO hydration_id;
ALTER TABLE rejections RENAME COLUMN reading_id TO hydration_id;

CREATE INDEX hydrations_by_item ON hydrations (source_id, entry_id, event_id);
CREATE INDEX refers_to_by_hydration ON refers_to (hydration_id);
CREATE INDEX span_names_by_hydration ON span_names (hydration_id);
CREATE INDEX speakers_by_hydration ON speakers (hydration_id);
CREATE INDEX rejections_by_hydration ON rejections (hydration_id);
CREATE INDEX candidate_hydrations_by_candidate ON candidate_hydrations (candidate_id);

CREATE TRIGGER events_not_rewritten BEFORE UPDATE ON events
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER hydrations_not_rewritten BEFORE UPDATE ON hydrations
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER hydrations_not_removed BEFORE DELETE ON hydrations
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER candidates_not_rewritten BEFORE UPDATE ON candidates
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER candidate_hydrations_not_rewritten BEFORE UPDATE ON candidate_hydrations
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER candidate_hydrations_not_removed BEFORE DELETE ON candidate_hydrations
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;

CREATE TRIGGER model_calls_not_rewritten BEFORE UPDATE ON model_calls
BEGIN
    SELECT raise(ABORT, 'a kept model call is never rewritten');
END;

CREATE TRIGGER conversations_not_rewritten BEFORE UPDATE ON conversations
BEGIN
    SELECT raise(ABORT, 'a kept conversation is never rewritten');
END;
