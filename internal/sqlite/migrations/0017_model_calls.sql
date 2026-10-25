-- The texts model calls are sent (instructions, schemas as JSON, inputs),
-- each kept once under the hex SHA-256 of its text: an act sends the same
-- instructions and schema on every call, and a retried request the same
-- input, so a call refers to its texts rather than repeating them.
CREATE TABLE call_texts (
    hash TEXT PRIMARY KEY,
    text TEXT NOT NULL
) STRICT;

-- Every model call an act makes, as it was sent and as it came back. A side
-- record: no act is shown it, and a kept call is never rewritten or removed.
CREATE TABLE model_calls (
    id INTEGER PRIMARY KEY,
    act TEXT NOT NULL,
    work_key TEXT NOT NULL, -- the piece of work, keyed as batch_requests' is
    at TEXT NOT NULL,
    batch_id TEXT NOT NULL, -- empty for a call made in real time
    model TEXT NOT NULL, -- the model asked for
    request_hash TEXT NOT NULL,
    instructions TEXT NOT NULL REFERENCES call_texts (hash),
    schema TEXT NOT NULL REFERENCES call_texts (hash),
    input TEXT NOT NULL REFERENCES call_texts (hash),
    precedents TEXT NOT NULL, -- a JSON array of the Feedback ids shown
    -- The response, or nulls when none came: in the provider's own form,
    -- the output the act read from it, the model the provider reports, why
    -- it stopped and its usage.
    response TEXT,
    output TEXT,
    reported_model TEXT,
    stop_reason TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cache_read_tokens INTEGER,
    outcome TEXT NOT NULL CHECK (outcome IN ('applied', 'refused', 'failed')),
    why TEXT NOT NULL
) STRICT;

CREATE INDEX model_calls_by_work ON model_calls (act, work_key, id);

CREATE TRIGGER model_calls_not_rewritten BEFORE UPDATE ON model_calls
BEGIN
    SELECT raise(ABORT, 'a kept model call is never rewritten');
END;

CREATE TRIGGER model_calls_not_removed BEFORE DELETE ON model_calls
BEGIN
    SELECT raise(ABORT, 'a kept model call is never removed');
END;

-- A pending request as sent, for its call: the texts it was sent, the model
-- asked for and the Feedback it showed. Null for a request submitted before
-- requests were kept.
ALTER TABLE batch_requests ADD COLUMN model TEXT;
ALTER TABLE batch_requests ADD COLUMN instructions TEXT REFERENCES call_texts (hash);
ALTER TABLE batch_requests ADD COLUMN schema TEXT REFERENCES call_texts (hash);
ALTER TABLE batch_requests ADD COLUMN input TEXT REFERENCES call_texts (hash);
ALTER TABLE batch_requests ADD COLUMN precedents TEXT;
