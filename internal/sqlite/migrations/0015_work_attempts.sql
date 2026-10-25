-- The failed attempts at each piece of work an act runs in batches (an act on
-- an Item, keyed as batch_requests' work_key is), with when and why it last
-- failed. Once failed is set the work has used its attempts: it is no longer
-- awaiting its act, and is shown to the Owner until the retry admin command
-- clears it. A row with failed unset is work still being tried.
CREATE TABLE work_attempts (
    act TEXT NOT NULL,
    work_key TEXT NOT NULL,
    attempts INTEGER NOT NULL CHECK (attempts > 0),
    last_failed_at TEXT NOT NULL,
    last_error TEXT NOT NULL,
    failed INTEGER NOT NULL CHECK (failed IN (0, 1)),
    PRIMARY KEY (act, work_key)
) STRICT;
