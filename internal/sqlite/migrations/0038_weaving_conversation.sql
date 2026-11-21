-- A weaving run is a conversation (system design: A weaving run is shown
-- what is new, and finds for itself what bears on it): each turn is a
-- request of its own, in a batch, carrying the tools the model may call and
-- the conversation so far after its input. A kept call and a pending
-- request keep both as one text, its JSON ({"Tools": ..., "Messages": ...}),
-- in call_texts like the request's other texts; NULL for a request that is
-- no conversation, as every request before this was. A kept call keeps the
-- tools its response called too, as JSON; NULL for none. A turn that asked
-- is kept with the outcome 'continued': model_calls is rebuilt by SQLite's
-- procedure to allow it, every call copied as it was, its triggers made
-- again as they were.
DROP TRIGGER model_calls_not_rewritten;
DROP TRIGGER model_calls_not_removed;

CREATE TABLE new_model_calls (
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
    outcome TEXT NOT NULL CHECK (outcome IN ('applied', 'refused', 'failed', 'continued')),
    why TEXT NOT NULL,
    effort TEXT NOT NULL DEFAULT '',
    conversation TEXT REFERENCES call_texts (hash),
    calls TEXT
) STRICT;

INSERT INTO new_model_calls (id, act, work_key, at, batch_id, model, request_hash, instructions, schema, input,
    precedents, response, output, reported_model, stop_reason, input_tokens, output_tokens, cache_read_tokens,
    outcome, why, effort)
SELECT id, act, work_key, at, batch_id, model, request_hash, instructions, schema, input,
    precedents, response, output, reported_model, stop_reason, input_tokens, output_tokens, cache_read_tokens,
    outcome, why, effort
FROM model_calls;

DROP TABLE model_calls;
ALTER TABLE new_model_calls RENAME TO model_calls;

CREATE INDEX model_calls_by_work ON model_calls (act, work_key, id);

CREATE TRIGGER model_calls_not_rewritten BEFORE UPDATE ON model_calls
BEGIN
    SELECT raise(ABORT, 'a kept model call is never rewritten');
END;

CREATE TRIGGER model_calls_not_removed BEFORE DELETE ON model_calls
BEGIN
    SELECT raise(ABORT, 'a kept model call is never removed');
END;

ALTER TABLE batch_requests ADD COLUMN conversation TEXT REFERENCES call_texts (hash);

-- A run's conversation as it stands after each turn that asked the Record:
-- the next turn is built from its newest, while the run has been neither
-- written nor marked failed. conversation is editor.WeavingTurns as JSON.
-- Work state, like a pending batch, not an event: what the run was shown
-- and asked is kept with its calls.
CREATE TABLE weaving_turns (
    work_key TEXT NOT NULL,
    turn INTEGER NOT NULL,
    at TEXT NOT NULL,
    conversation TEXT NOT NULL,
    PRIMARY KEY (work_key, turn)
) STRICT;

-- The most a run the Owner asked for by the weave command may spend, in
-- dollars at batch rates, over all its turns.
CREATE TABLE weaving_caps (
    work_key TEXT PRIMARY KEY,
    max_usd REAL NOT NULL
) STRICT;
