-- An Editor's Span can cite an earlier Editor's Span, holding it by its
-- address without copying it (system design: The Editor writes Spans). That
-- address is the Story that wrote it and its place n among that Story's
-- Editor's Spans; the words stay in the Judgement that wrote them. Its
-- citation shares m, the order cited, with the citing Span's citations of
-- Sources' Spans in editor_span_cites, so each m of a citing Span is in one
-- of the two tables.
CREATE TABLE editor_span_cites_editor (
    story_id INTEGER NOT NULL,
    n INTEGER NOT NULL,
    m INTEGER NOT NULL,
    cited_story_id INTEGER NOT NULL,
    cited_n INTEGER NOT NULL,
    PRIMARY KEY (story_id, n, m),
    CHECK (cited_story_id < story_id),
    FOREIGN KEY (story_id, n) REFERENCES editor_spans (story_id, n),
    FOREIGN KEY (cited_story_id, cited_n) REFERENCES editor_spans (story_id, n)
) STRICT;

CREATE INDEX editor_span_cites_editor_by_cited ON editor_span_cites_editor (cited_story_id, cited_n);

CREATE TRIGGER editor_span_cites_editor_not_rewritten BEFORE UPDATE ON editor_span_cites_editor
BEGIN
    SELECT raise(ABORT, 'an event is never rewritten');
END;

CREATE TRIGGER editor_span_cites_editor_not_removed BEFORE DELETE ON editor_span_cites_editor
BEGIN
    SELECT raise(ABORT, 'an event is never removed');
END;
