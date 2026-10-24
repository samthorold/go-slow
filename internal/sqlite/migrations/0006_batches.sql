-- The batches the daily acts have submitted to a provider and not yet seen
-- end, kept so that a restart picks them up. A batch is removed once its
-- results are applied.
CREATE TABLE batches (
    id TEXT PRIMARY KEY, -- the provider's identifier
    submitted_at TEXT NOT NULL
) STRICT;

-- Each request in a pending batch: its identifier, and the act and work
-- (such as an Item, for reading) it is for.
CREATE TABLE batch_requests (
    batch_id TEXT NOT NULL REFERENCES batches (id) ON DELETE CASCADE,
    request_id TEXT NOT NULL,
    act TEXT NOT NULL,
    work_key TEXT NOT NULL,
    PRIMARY KEY (batch_id, request_id)
) STRICT;
