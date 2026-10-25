package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/interest"
)

// LastBriefingWritten is when the latest Briefing was written, or zero.
func (s *Store) LastBriefingWritten(ctx context.Context) (time.Time, error) {
	var at string
	if err := s.db.QueryRowContext(ctx, `SELECT coalesce(max(written_at), '') FROM briefings`).Scan(&at); err != nil {
		return time.Time{}, fmt.Errorf("last briefing: %w", err)
	}
	return parseTime(at), nil
}

// ToBrief are the Stories with Developments placed in (after, upTo] whose
// Facts come from Items a Topic ties, each with only those Developments, the
// names of those Topics, and the closest of their Depths.
func (s *Store) ToBrief(ctx context.Context, after, upTo time.Time) ([]editor.ToBrief, error) {
	ids, byStory, err := s.selected(ctx, after, upTo)
	if err != nil {
		return nil, fmt.Errorf("to brief: %w", err)
	}
	out := make([]editor.ToBrief, 0, len(ids))
	for _, id := range ids {
		st, _, err := s.story(ctx, id, false)
		if err != nil {
			return nil, err
		}
		p := byStory[id]
		slices.Sort(p.topics)
		tb := editor.ToBrief{Story: st, Topics: p.topics, Depth: p.depth}
		for _, d := range st.Timeline {
			if slices.Contains(p.facts, d.ID) {
				tb.Today = append(tb.Today, d)
			}
		}
		out = append(out, tb)
	}
	return out, nil
}

// picked is what a Story has selected for the Briefing: its Developments'
// Facts, and the Topics, and closest Depth, that tie their Items.
type picked struct {
	facts  []int64
	depth  interest.Depth
	topics []string
}

// selected are the Stories with Developments placed in (after, upTo] from
// Items a Topic ties, in id order, and what each has selected.
func (s *Store) selected(ctx context.Context, after, upTo time.Time) ([]int64, map[int64]*picked, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT developments.story_id, developments.fact_id, max(topics.depth),
			json_group_array(DISTINCT topics.name)
		FROM developments
			JOIN fact_placings ON fact_placings.fact_id = developments.fact_id
			JOIN facts ON facts.id = developments.fact_id
			JOIN item_topics ON item_topics.source_id = facts.source_id AND item_topics.entry_id = facts.entry_id
			JOIN topics ON topics.id = item_topics.topic_id
		WHERE fact_placings.placed_at > ? AND fact_placings.placed_at <= ?
			-- Feedback keeps out what it withdrew, and what the Owner
			-- does not want in the Briefing.
			AND developments.withdrawn_by IS NULL AND developments.unwanted_by IS NULL
			AND NOT EXISTS (SELECT 1 FROM stories WHERE stories.id = developments.story_id AND stories.unwanted_by IS NOT NULL)
		GROUP BY developments.story_id, developments.fact_id
		ORDER BY developments.story_id`, formatTime(after), formatTime(upTo))
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	byStory := map[int64]*picked{}
	for rows.Next() {
		var storyID, factID int64
		var depth interest.Depth
		var names string
		if err := rows.Scan(&storyID, &factID, &depth, &names); err != nil {
			return nil, nil, err
		}
		var topics []string
		if err := json.Unmarshal([]byte(names), &topics); err != nil {
			return nil, nil, err
		}
		p, ok := byStory[storyID]
		if !ok {
			p = &picked{}
			byStory[storyID] = p
			ids = append(ids, storyID)
		}
		p.facts = append(p.facts, factID)
		p.depth = max(p.depth, depth)
		for _, t := range topics {
			if !slices.Contains(p.topics, t) {
				p.topics = append(p.topics, t)
			}
		}
	}
	return ids, byStory, rows.Err()
}

// SaveBriefing keeps a Briefing: its Stories in order, and each statement
// with the Developments it cites, all at once.
func (s *Store) SaveBriefing(ctx context.Context, b editor.Briefing) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO briefings (day, written_at, model) VALUES (?, ?, ?)`,
		b.Day, formatTime(b.WrittenAt), b.Model); err != nil {
		return fmt.Errorf("save briefing %s: %w", b.Day, err)
	}
	for i, bs := range b.Stories {
		if _, err := tx.ExecContext(ctx, `INSERT INTO briefing_stories (day, position, story_id) VALUES (?, ?, ?)`,
			b.Day, i, bs.Story.ID); err != nil {
			return fmt.Errorf("save briefing %s: story %d: %w", b.Day, bs.Story.ID, err)
		}
		for j, st := range bs.Statements {
			var id int64
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO briefing_statements (day, story_id, position, text) VALUES (?, ?, ?, ?) RETURNING id`,
				b.Day, bs.Story.ID, j, st.Text).Scan(&id); err != nil {
				return fmt.Errorf("save briefing %s: statement: %w", b.Day, err)
			}
			for k, d := range st.Developments {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO statement_developments (statement_id, story_id, fact_id, position) VALUES (?, ?, ?, ?)`,
					id, bs.Story.ID, d.ID, k); err != nil {
					return fmt.Errorf("save briefing %s: citation of fact %d: %w", b.Day, d.ID, err)
				}
			}
		}
	}
	return tx.Commit()
}

// SaveBriefingFailure keeps a failure to write a day's Briefing.
func (s *Store) SaveBriefingFailure(ctx context.Context, f editor.BriefingFailure) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO briefing_failures (day, failed_at, reason) VALUES (?, ?, ?)`,
		f.Day, formatTime(f.At), f.Reason); err != nil {
		return fmt.Errorf("save briefing failure: %w", err)
	}
	return nil
}

// BriefingDays are the days a Briefing was written for, and the first day
// one was written or tried, or empty if none ever was.
func (s *Store) BriefingDays(ctx context.Context) ([]string, string, error) {
	var first string
	if err := s.db.QueryRowContext(ctx, `
		SELECT coalesce(min(day), '') FROM (SELECT day FROM briefings UNION SELECT day FROM briefing_failures)`,
	).Scan(&first); err != nil {
		return nil, "", fmt.Errorf("briefing days: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT day FROM briefings ORDER BY day`)
	if err != nil {
		return nil, "", fmt.Errorf("briefing days: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var days []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, "", fmt.Errorf("briefing days: %w", err)
		}
		days = append(days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("briefing days: %w", err)
	}
	return days, first, nil
}

// BriefingFailure is the latest failure to write the day's Briefing,
// reporting whether there is one.
func (s *Store) BriefingFailure(ctx context.Context, day string) (editor.BriefingFailure, bool, error) {
	f := editor.BriefingFailure{Day: day}
	var at string
	err := s.db.QueryRowContext(ctx, `
		SELECT failed_at, reason FROM briefing_failures WHERE day = ? ORDER BY failed_at DESC, id DESC LIMIT 1`,
		day).Scan(&at, &f.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return f, false, nil
	}
	if err != nil {
		return f, false, fmt.Errorf("briefing failure: %w", err)
	}
	f.At = parseTime(at)
	return f, true, nil
}

// TakenOutBy are the ids of the Feedback that takes something of the day's
// written Briefing out of it: an unwanted Story it tells, or a Development
// one of its statements cites, withdrawn or unwanted. In id order.
func (s *Store) TakenOutBy(ctx context.Context, day string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stories.unwanted_by FROM briefing_stories JOIN stories ON stories.id = briefing_stories.story_id
		WHERE briefing_stories.day = ?1 AND stories.unwanted_by IS NOT NULL
		UNION
		SELECT d.withdrawn_by
		FROM briefing_statements st
			JOIN statement_developments cited ON cited.statement_id = st.id
			JOIN developments d ON d.story_id = cited.story_id AND d.fact_id = cited.fact_id
		WHERE st.day = ?1 AND d.withdrawn_by IS NOT NULL
		UNION
		SELECT d.unwanted_by
		FROM briefing_statements st
			JOIN statement_developments cited ON cited.statement_id = st.id
			JOIN developments d ON d.story_id = cited.story_id AND d.fact_id = cited.fact_id
		WHERE st.day = ?1 AND d.unwanted_by IS NOT NULL
		ORDER BY 1`, day)
	if err != nil {
		return nil, fmt.Errorf("taken out of briefing %s: %w", day, err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("taken out of briefing %s: %w", day, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Briefing is the day's Briefing, reporting whether one was written: its
// Stories in order, and each statement with the Developments it cites,
// traced to their Facts and Items.
func (s *Store) Briefing(ctx context.Context, day string) (editor.Briefing, bool, error) {
	b := editor.Briefing{Day: day}
	var at string
	err := s.db.QueryRowContext(ctx, `SELECT written_at, model FROM briefings WHERE day = ?`, day).Scan(&at, &b.Model)
	if errors.Is(err, sql.ErrNoRows) {
		return b, false, nil
	}
	if err != nil {
		return b, false, fmt.Errorf("briefing %s: %w", day, err)
	}
	b.WrittenAt = parseTime(at)
	rows, err := s.db.QueryContext(ctx, `
		SELECT briefing_stories.story_id, stories.title, briefing_statements.id, briefing_statements.text,
			facts.id, facts.source_id, facts.entry_id, facts.statement, facts.as_at, facts.quote,
			`+factEntities+`, items.title
		FROM briefing_stories
			JOIN stories ON stories.id = briefing_stories.story_id
			JOIN briefing_statements USING (day, story_id)
			JOIN statement_developments ON statement_developments.statement_id = briefing_statements.id
			JOIN facts ON facts.id = statement_developments.fact_id
			JOIN items ON items.source_id = facts.source_id AND items.entry_id = facts.entry_id
		WHERE briefing_stories.day = ? AND stories.unwanted_by IS NULL
			-- Feedback takes out of the Briefing every statement that rests
			-- on a Development withdrawn, or not wanted.
			AND NOT EXISTS (SELECT 1 FROM statement_developments cited
				JOIN developments d ON d.story_id = cited.story_id AND d.fact_id = cited.fact_id
				WHERE cited.statement_id = briefing_statements.id
					AND (d.withdrawn_by IS NOT NULL OR d.unwanted_by IS NOT NULL))
		ORDER BY briefing_stories.position, briefing_statements.position, statement_developments.position`, day)
	if err != nil {
		return b, false, fmt.Errorf("briefing %s: %w", day, err)
	}
	defer func() { _ = rows.Close() }()
	var lastStatement int64
	for rows.Next() {
		var storyID, statementID int64
		var title, text, asAt, ents string
		var d editor.Development
		if err := rows.Scan(&storyID, &title, &statementID, &text,
			&d.ID, &d.SourceID, &d.EntryID, &d.Statement, &asAt, &d.Quote, &ents, &d.ItemTitle); err != nil {
			return b, false, fmt.Errorf("briefing %s: %w", day, err)
		}
		if err := scanFact(&d.Fact, asAt, ents); err != nil {
			return b, false, fmt.Errorf("briefing %s: %w", day, err)
		}
		if n := len(b.Stories); n == 0 || b.Stories[n-1].Story.ID != storyID {
			b.Stories = append(b.Stories, editor.BriefedStory{Story: editor.Story{ID: storyID, Title: title}})
		}
		bs := &b.Stories[len(b.Stories)-1]
		if statementID != lastStatement {
			bs.Statements = append(bs.Statements, editor.Statement{Text: text})
			lastStatement = statementID
		}
		st := &bs.Statements[len(bs.Statements)-1]
		st.Developments = append(st.Developments, d)
	}
	if err := rows.Err(); err != nil {
		return b, false, fmt.Errorf("briefing %s: %w", day, err)
	}
	return b, true, nil
}
