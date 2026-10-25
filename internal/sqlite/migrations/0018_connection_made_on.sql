-- The Item whose connecting made a Connection, as its work key (Source and
-- entry a line apart), so the Connection links to the call that made it:
-- several Items are connected at the same time in one run, and either of a
-- Connection's Facts may be the Item's. Null for a Connection made before
-- it was kept.
ALTER TABLE fact_connections ADD COLUMN made_on TEXT;
