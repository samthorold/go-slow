-- The Feedback an act was shown as precedent when it judged an Item, kept
-- with its result, so the reason for a judgement stays visible in what the
-- act was shown. rank is the order it was shown in, the most relevant first.
CREATE TABLE precedents_shown (
    act TEXT NOT NULL,
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    feedback_id INTEGER NOT NULL REFERENCES feedback (id),
    rank INTEGER NOT NULL,
    shown_at TEXT NOT NULL,
    PRIMARY KEY (act, source_id, entry_id, feedback_id),
    FOREIGN KEY (source_id, entry_id) REFERENCES items (source_id, entry_id)
) STRICT;
