-- Every conversation an act has with a model (the Sunday read's agent),
-- kept turn by turn: one attempt at its piece of work. A side record, like
-- model_calls: no act is shown it, and it is never rewritten or removed.
-- The act's instructions and tools, sent on every turn, are kept once in
-- call_texts.
CREATE TABLE conversations (
    id INTEGER PRIMARY KEY,
    act TEXT NOT NULL,
    work_key TEXT NOT NULL, -- the piece of work: the Sunday, for the Sunday read
    at TEXT NOT NULL, -- when it ended
    model TEXT NOT NULL, -- the model asked for
    instructions TEXT NOT NULL REFERENCES call_texts (hash),
    tools TEXT NOT NULL REFERENCES call_texts (hash), -- as JSON
    outcome TEXT NOT NULL CHECK (outcome IN ('applied', 'failed')),
    why TEXT NOT NULL
) STRICT;

-- Each turn keeps only the messages new with its request (the act's opening
-- message, or the tool results of the turn before), so the conversation's
-- repeated prefix is not kept again on every turn: a turn's whole request
-- is the conversation's instructions and tools and, for each turn up to it,
-- what it sent and what came back.
CREATE TABLE conversation_turns (
    conversation_id INTEGER NOT NULL REFERENCES conversations (id),
    turn INTEGER NOT NULL, -- from 1
    at TEXT NOT NULL,
    request_hash TEXT NOT NULL, -- of the whole request as sent, and the model
    sent TEXT NOT NULL, -- the messages new with the request, as JSON
    -- The model's turn, or nulls when none came: in the provider's own form,
    -- its text and tool calls (JSON) as the act read them, the model the
    -- provider reports, why it stopped and its usage.
    response TEXT,
    text TEXT,
    calls TEXT,
    reported_model TEXT,
    stop_reason TEXT,
    input_tokens INTEGER,
    output_tokens INTEGER,
    cache_read_tokens INTEGER,
    outcome TEXT NOT NULL CHECK (outcome IN ('continued', 'applied', 'refused', 'failed')),
    why TEXT NOT NULL,
    PRIMARY KEY (conversation_id, turn)
) STRICT;

CREATE TRIGGER conversations_not_rewritten BEFORE UPDATE ON conversations
BEGIN
    SELECT raise(ABORT, 'a kept conversation is never rewritten');
END;

CREATE TRIGGER conversations_not_removed BEFORE DELETE ON conversations
BEGIN
    SELECT raise(ABORT, 'a kept conversation is never removed');
END;

CREATE TRIGGER conversation_turns_not_rewritten BEFORE UPDATE ON conversation_turns
BEGIN
    SELECT raise(ABORT, 'a kept turn is never rewritten');
END;

CREATE TRIGGER conversation_turns_not_removed BEFORE DELETE ON conversation_turns
BEGIN
    SELECT raise(ABORT, 'a kept turn is never removed');
END;

-- The conversation each Sunday read, and each failure to write one, came
-- from; null for one with no model call, or kept before conversations were.
ALTER TABLE sunday_reads ADD COLUMN conversation_id INTEGER REFERENCES conversations (id);
ALTER TABLE sunday_read_failures ADD COLUMN conversation_id INTEGER REFERENCES conversations (id);
