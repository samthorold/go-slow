package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// send makes a request with a form body, as htmx sends one.
func send(t *testing.T, app http.Handler, method, path string, form url.Values) response {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	app.ServeHTTP(rec, req)
	return response{rec.Code, rec.Header(), rec.Body.String()}
}

func TestTheOwnerAddsATopicAndSeesItOnTheTopicsPage(t *testing.T) {
	app := newApp(t, saturday)

	res := send(t, app, http.MethodPost, "/topics", url.Values{
		"name":  {"Reform UK"},
		"depth": {"close"},
		"terms": {"Reform UK\nFarage"},
	})

	if res.status != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303:\n%s", res.status, res.body)
	}
	if loc := res.header.Get("Location"); loc != "/topics" {
		t.Errorf("Location = %q, want /topics", loc)
	}
	page := get(t, app, "/topics")
	if page.status != http.StatusOK {
		t.Fatalf("GET /topics: status = %d", page.status)
	}
	for _, want := range []string{"<!doctype html>", "Reform UK", "Farage", `<button name="depth" value="close" aria-pressed="true">`} {
		if !strings.Contains(page.body, want) {
			t.Errorf("Topics page lacks %q:\n%s", want, page.body)
		}
	}
}

func TestATopicThatFailsValidationReRendersTheFormWithItsErrors(t *testing.T) {
	app := newApp(t, saturday)
	if res := send(t, app, http.MethodPost, "/topics", url.Values{
		"name": {"Reform UK"}, "depth": {"close"}, "terms": {"Reform UK"},
	}); res.status != http.StatusSeeOther {
		t.Fatalf("first add: status = %d", res.status)
	}

	res := send(t, app, http.MethodPost, "/topics", url.Values{
		"name":  {"reform uk "},
		"depth": {"obsessive"},
		"terms": {"Farage"},
	})

	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.status)
	}
	for _, want := range []string{
		"<form",
		"already a Topic",
		"Choose a Depth",
		">Farage</textarea>",
		`value="reform uk "`,
	} {
		if !strings.Contains(res.body, want) {
			t.Errorf("re-rendered form lacks %q:\n%s", want, res.body)
		}
	}
}

// addTopic takes up a Topic through the Topics page.
func addTopic(t *testing.T, app http.Handler, form url.Values) {
	t.Helper()
	if res := send(t, app, http.MethodPost, "/topics", form); res.status != http.StatusSeeOther {
		t.Fatalf("add Topic: status = %d:\n%s", res.status, res.body)
	}
}

func TestTheOwnerChangesATopicsDepth(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{"name": {"Reform UK"}, "depth": {"passing"}, "terms": {"Reform UK"}})

	res := send(t, app, http.MethodPut, "/topics/1", url.Values{"depth": {"close"}})

	if res.status != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303:\n%s", res.status, res.body)
	}
	if loc := res.header.Get("Location"); loc != "/topics" {
		t.Errorf("Location = %q, want /topics", loc)
	}
	page := get(t, app, "/topics")
	if !strings.Contains(page.body, `<button name="depth" value="close" aria-pressed="true">`) {
		t.Errorf("Topic is not shown at its new Depth:\n%s", page.body)
	}
}

func TestAnUnknownDepthIsRefusedWith422(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{"name": {"Reform UK"}, "depth": {"passing"}, "terms": {"Reform UK"}})

	res := send(t, app, http.MethodPut, "/topics/1", url.Values{"depth": {"obsessive"}})

	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.status)
	}
	if !strings.Contains(topicRow(res.body, 1), "Choose a Depth") {
		t.Errorf("the Topic's row does not say why:\n%s", res.body)
	}
}

func TestChangingTheDepthOfAnUnknownTopicIsNotFound(t *testing.T) {
	app := newApp(t, saturday)

	res := send(t, app, http.MethodPut, "/topics/7", url.Values{"depth": {"close"}})

	if res.status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.status)
	}
}

func TestTheOwnerRemovesATopic(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{"name": {"Reform UK"}, "depth": {"passing"}, "terms": {"Reform UK"}})
	page := get(t, app, "/topics")
	if !strings.Contains(page.body, `hx-delete="/topics/1"`) || !strings.Contains(page.body, "hx-confirm=") {
		t.Fatalf("Topics page has no confirmed Remove control:\n%s", page.body)
	}

	res := send(t, app, http.MethodDelete, "/topics/1", nil)

	// Removing an element is 200 with an empty body, swapped outerHTML.
	if res.status != http.StatusOK || res.body != "" {
		t.Fatalf("status = %d, body = %q; want 200 and empty", res.status, res.body)
	}
	if page := get(t, app, "/topics"); strings.Contains(page.body, "Reform UK") {
		t.Errorf("removed Topic still listed:\n%s", page.body)
	}
	if res := send(t, app, http.MethodDelete, "/topics/1", nil); res.status != http.StatusNotFound {
		t.Errorf("removing it again: status = %d, want 404", res.status)
	}
}

func TestWhatTheOwnerTypedIsInertOnTheTopicsPage(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{
		"name":  {`<span hx-get="/evil" hx-trigger="load">Reform</span>`},
		"depth": {"close"},
		"terms": {`<b hx-delete="/">Farage</b>`},
	})

	res := get(t, app, "/topics")

	if strings.Contains(res.body, `<span hx-get`) || strings.Contains(res.body, `<b hx-delete`) {
		t.Errorf("typed markup was not escaped:\n%s", res.body)
	}
	if !strings.Contains(res.body, `<strong hx-disable>`) {
		t.Errorf("the Topic's name is not inside hx-disable:\n%s", res.body)
	}
}

// topicRow is the row of the Followed table for Topic id, or "" if none.
func topicRow(body string, id int) string {
	return regexp.MustCompile(`(?s)<tr id="topic-` + strconv.Itoa(id) + `"[^>]*>.*?</tr>`).FindString(body)
}

func TestFollowedTopicsRenderAsATableOfTopicDepthTermsAndSources(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{
		"name": {"Reform UK"}, "depth": {"close"}, "terms": {"Reform UK\nFarage"}, "source": {"govuk-number-10"},
	})

	res := get(t, app, "/topics")

	head := regexp.MustCompile(`(?s)<thead>.*?</thead>`).FindString(res.body)
	cols := regexp.MustCompile(`<th[^>]*>([^<]*)</th>`).FindAllStringSubmatch(head, -1)
	var got []string
	for _, c := range cols {
		got = append(got, c[1])
	}
	if want := []string{"Topic", "Depth", "Terms", "Sources", ""}; !slices.Equal(got, want) {
		t.Errorf("the Followed table's columns are %q, want %q:\n%s", got, want, res.body)
	}
	row := topicRow(res.body, 1)
	if row == "" {
		t.Fatalf("no row for the Topic:\n%s", res.body)
	}
	cells := regexp.MustCompile(`(?s)<td([^>]*)>(.*?)</td>`).FindAllStringSubmatch(row, -1)
	if len(cells) != 5 {
		t.Fatalf("the row has %d cells, want 5:\n%s", len(cells), row)
	}
	if !strings.Contains(cells[0][2], "Reform UK") {
		t.Errorf("the Topic cell lacks the name:\n%s", cells[0][2])
	}
	for i, want := range map[int]string{2: "Reform UK, Farage", 3: "govuk-number-10"} {
		if !strings.Contains(cells[i][2], want) {
			t.Errorf("cell %d lacks %q:\n%s", i, want, cells[i][2])
		}
		if !strings.Contains(cells[i][1], `class="quiet"`) {
			t.Errorf("cell %d is not in the quiet style: <td%s>", i, cells[i][1])
		}
	}
}

// A row's Depth is a button per Depth that saves on click, the current one
// pressed, with no other button to submit it.
func TestATopicsDepthIsAButtonPerDepthThatSavesOnClick(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{"name": {"Reform UK"}, "depth": {"passing"}, "terms": {"Reform UK"}})

	row := topicRow(get(t, app, "/topics").body, 1)

	form := regexp.MustCompile(`(?s)<form hx-put="/topics/1"[^>]*>.*?</form>`).FindString(row)
	if form == "" {
		t.Fatalf("the row has no form putting its Depth:\n%s", row)
	}
	buttons := regexp.MustCompile(`<button[^>]*>`).FindAllString(form, -1)
	if len(buttons) != 3 {
		t.Fatalf("the Depth form has %d buttons, want one per Depth:\n%s", len(buttons), form)
	}
	for i, d := range []string{"passing", "following", "close"} {
		pressed := strconv.FormatBool(d == "passing")
		for _, want := range []string{`name="depth"`, `value="` + d + `"`, `aria-pressed="` + pressed + `"`} {
			if !strings.Contains(buttons[i], want) {
				t.Errorf("the %s button lacks %s: %s", d, want, buttons[i])
			}
		}
	}
	if strings.Contains(form, "<select") || strings.Contains(form, "Change Depth") {
		t.Errorf("the Depth form still has a select or a separate button:\n%s", form)
	}
}

// Remove is in the row's actions, hidden at rest in a way that keeps it in
// the tab order and shown while the pointer or keyboard focus is in the row,
// as the Briefing's controls are.
func TestRemoveIsInTheRowsActionsShownOnHoverOrFocus(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{"name": {"Reform UK"}, "depth": {"passing"}, "terms": {"Reform UK"}})

	row := topicRow(get(t, app, "/topics").body, 1)

	actions := regexp.MustCompile(`(?s)<div class="actions[^"]*"[^>]*>(.*?)</div>`).FindStringSubmatch(row)
	if actions == nil || !strings.Contains(actions[1], `hx-delete="/topics/1"`) || !strings.Contains(actions[1], "hx-confirm=") {
		t.Fatalf("the row's actions hold no confirmed Remove:\n%s", row)
	}
	css := get(t, app, "/static/go-slow.css").body
	rest := regexp.MustCompile(`(?m)^\.topics \.actions\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if rest == nil {
		t.Fatalf("no rule hides the Topics' actions at rest:\n%s", css)
	}
	if strings.Contains(rest[1], "display: none") || strings.Contains(rest[1], "visibility: hidden") {
		t.Errorf("the actions are hidden at rest so the keyboard cannot reach them:\n%s", rest[0])
	}
	for _, sel := range []string{".topics tr:hover .actions", ".topics tr:focus-within .actions"} {
		if !strings.Contains(css, sel) {
			t.Errorf("no rule reveals the actions on %s", sel)
		}
	}
}

// takeUp matches the disclosure holding the form that takes up a Topic.
var takeUp = regexp.MustCompile(`(?s)<details([^>]*)>\s*<summary>Take up a Topic</summary>.*?</details>`)

func TestTakingUpATopicIsCollapsedAboveTheTable(t *testing.T) {
	app := newApp(t, saturday)
	addTopic(t, app, url.Values{"name": {"Reform UK"}, "depth": {"passing"}, "terms": {"Reform UK"}})

	res := get(t, app, "/topics")

	m := takeUp.FindStringSubmatchIndex(res.body)
	if m == nil {
		t.Fatalf("no disclosure to take up a Topic:\n%s", res.body)
	}
	if attrs := res.body[m[2]:m[3]]; strings.Contains(attrs, "open") {
		t.Errorf("taking up a Topic is open at rest: <details%s>", attrs)
	}
	if !strings.Contains(res.body[m[0]:m[1]], `<form method="post" action="/topics"`) {
		t.Errorf("the disclosure does not hold the form:\n%s", res.body[m[0]:m[1]])
	}
	if table := strings.Index(res.body, "<table"); table < m[0] {
		t.Errorf("the disclosure is not above the table:\n%s", res.body)
	}
}

func TestAFailedTakeUpReRendersItOpen(t *testing.T) {
	app := newApp(t, saturday)

	res := send(t, app, http.MethodPost, "/topics", url.Values{"name": {"Reform UK"}, "depth": {"obsessive"}, "terms": {"Farage"}})

	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.status)
	}
	m := takeUp.FindStringSubmatch(res.body)
	if m == nil {
		t.Fatalf("no disclosure to take up a Topic:\n%s", res.body)
	}
	if !strings.Contains(m[1], "open") {
		t.Errorf("a failed take-up is re-rendered closed: <details%s>", m[1])
	}
	if !strings.Contains(m[0], "Choose a Depth") {
		t.Errorf("the disclosure does not hold the errors:\n%s", m[0])
	}
}

func TestATopicMustTieSomething(t *testing.T) {
	app := newApp(t, saturday)

	res := send(t, app, http.MethodPost, "/topics", url.Values{
		"name": {"Standards in public life"}, "depth": {"following"},
	})

	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", res.status)
	}
	if !strings.Contains(res.body, "at least one term or Source") {
		t.Errorf("form does not say what is missing:\n%s", res.body)
	}
}
