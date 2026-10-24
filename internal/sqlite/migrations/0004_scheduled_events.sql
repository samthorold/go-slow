-- When a scheduled event (a release on a statistics body's release calendar)
-- is due; '' for any other Item. RFC 3339, UTC.
ALTER TABLE items ADD COLUMN due TEXT NOT NULL DEFAULT '';
