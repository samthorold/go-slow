package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
	for _, want := range []string{"<!doctype html>", "Reform UK", "Farage", `<option value="close" selected>`} {
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
	if !strings.Contains(page.body, `<option value="close" selected>`) {
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
	if !strings.Contains(res.body, "Choose a Depth") {
		t.Errorf("page does not say why:\n%s", res.body)
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
