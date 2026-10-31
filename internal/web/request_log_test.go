package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// requestLines are the lines the app logged for the requests it answered,
// each a JSON object as the log stream holds it.
func requestLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	dec := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	for dec.More() {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			t.Fatalf("a log line is not JSON: %v\n%s", err, logs)
		}
		if line["msg"] == "request" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestARequestToAPageLogsOneLineWithMethodPathStatusAndDuration(t *testing.T) {
	app, logs := newAppWithLog(t, saturday, openStore(t))

	get(t, app, "/briefings?page=2")

	lines := requestLines(t, logs)
	if len(lines) != 1 {
		t.Fatalf("logged %d request lines, want 1:\n%s", len(lines), logs)
	}
	line := lines[0]
	if line["level"] != "INFO" || line["method"] != http.MethodGet || line["path"] != "/briefings" ||
		line["status"] != float64(http.StatusOK) {
		t.Errorf("request line = %v, want INFO GET /briefings 200, the path without its query", line)
	}
	if _, ok := line["duration_ms"]; !ok {
		t.Errorf("request line has no duration: %v", line)
	}
}

func TestStaticFileRequestsAddNoInfoLevelLines(t *testing.T) {
	app, logs := newAppWithLog(t, saturday, openStore(t))

	if res := get(t, app, "/static/htmx-2.0.10.min.js"); res.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.status)
	}

	for _, line := range requestLines(t, logs) {
		if line["level"] != "DEBUG" {
			t.Errorf("a static file request logged above debug level: %v", line)
		}
	}
}

func TestAServerErrorLogsTheErrorBehindItOnItsRequestLine(t *testing.T) {
	store := openStore(t)
	app, logs := newAppWithLog(t, saturday, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	get(t, app, "/")

	lines := requestLines(t, logs)
	if len(lines) != 1 {
		t.Fatalf("logged %d request lines, want 1:\n%s", len(lines), logs)
	}
	line := lines[0]
	if line["level"] != "ERROR" || line["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("request line = %v, want ERROR 500", line)
	}
	if e, _ := line["error"].(string); !strings.Contains(e, "closed") {
		t.Errorf("request line does not carry the store's error: %v", line)
	}
}

func TestAPostOfFeedbackLogsItsStatusAndNeverItsFormValues(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeStoryBriefing(t, store, saturday, spoke)
	app, logs := newAppWithLog(t, saturday, store)

	send(t, app, http.MethodPost, today, url.Values{"kind": {"missed"}, "level": {"story"},
		"title": {"Scottish politics"}, "facts": {"1"}})
	send(t, app, http.MethodPost, today, url.Values{"kind": {"missed"}, "level": {"story"},
		"title": {" "}, "facts": {"1"}})

	lines := requestLines(t, logs)
	if len(lines) != 2 {
		t.Fatalf("logged %d request lines, want 2:\n%s", len(lines), logs)
	}
	for i, want := range []int{http.StatusSeeOther, http.StatusUnprocessableEntity} {
		if lines[i]["method"] != http.MethodPost || lines[i]["path"] != today || lines[i]["status"] != float64(want) {
			t.Errorf("request line %d = %v, want POST %s %d", i, lines[i], today, want)
		}
	}
	for _, typed := range []string{"Scottish politics", "missed"} {
		if strings.Contains(logs.String(), typed) {
			t.Errorf("the log holds the form value %q:\n%s", typed, logs)
		}
	}
}
