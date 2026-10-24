package collect

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// statementsSite is where Parliament publishes a written statement's page.
// Items link to it; Collection never fetches it.
const statementsSite = "https://questions-statements.parliament.uk/written-statements/detail/"

type writtenStatementsPage struct {
	Results []struct {
		Value json.RawMessage `json:"value"`
	} `json:"results"`
}

// writtenStatement is the part of a statement an Item's fields carry; the
// rest (who made it, the department, the House) is kept in Raw. The list
// gives the start of the statement's text, which becomes the Item's summary.
type writtenStatement struct {
	ID       int64  `json:"id"`
	UIN      string `json:"uin"`
	DateMade string `json:"dateMade"`
	Title    string `json:"title"`
	Text     string `json:"text"`
}

// london is where Parliament sits: dateMade is a date there, with no zone.
var london = func() *time.Location {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// parseWrittenStatements reads Parliament's Written Questions Service list of
// written statements. A statement with no id is skipped: the id is its
// identity.
func parseWrittenStatements(body []byte) ([]Item, error) {
	var page writtenStatementsPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parse written statements: %w", err)
	}
	var items []Item
	for _, r := range page.Results {
		var s writtenStatement
		if err := json.Unmarshal(r.Value, &s); err != nil {
			return nil, fmt.Errorf("parse written statement: %w", err)
		}
		if s.ID == 0 {
			continue
		}
		made, err := time.ParseInLocation("2006-01-02T15:04:05", strings.TrimSpace(s.DateMade), london)
		if err != nil {
			made = time.Time{}
		}
		link := ""
		if !made.IsZero() && s.UIN != "" {
			link = statementsSite + made.Format(time.DateOnly) + "/" + s.UIN
		}
		items = append(items, Item{
			EntryID:   strconv.FormatInt(s.ID, 10),
			Title:     strings.TrimSpace(s.Title),
			Link:      link,
			Summary:   strings.TrimSpace(s.Text),
			Published: made,
			Updated:   made,
			Raw:       string(r.Value),
		})
	}
	return items, nil
}
