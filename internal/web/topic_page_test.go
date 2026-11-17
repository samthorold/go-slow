package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// topicPage follows the Topics page's link to the Topic named name.
func topicPage(t *testing.T, app http.Handler, name string) response {
	t.Helper()
	list := get(t, app, "/topics").body
	m := regexp.MustCompile(`<a href="(/topics/\d+)"[^>]*>` + regexp.QuoteMeta(name) + `</a>`).FindStringSubmatch(list)
	if m == nil {
		t.Fatalf("the Topics page does not link %q to its page:\n%s", name, list)
	}
	return get(t, app, m[1])
}

func TestATopicsPageListsTheStoriesTouchingItNewestFirst(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)
	// Both Stories cite S1, "It’s good to be here in Motherwell."
	addTopic(t, app, url.Values{"name": {"Motherwell"}, "terms": {"Motherwell"}})

	res := topicPage(t, app, "Motherwell")

	if res.status != http.StatusOK || !strings.Contains(res.body, "<!doctype html>") {
		t.Fatalf("status %d, not a full page:\n%s", res.status, res.body)
	}
	newer := strings.Index(res.body, "Two billion is a down payment on devolution.")
	older := strings.Index(res.body, "Motherwell is where the money lands first, as it did a…")
	if newer < 0 || older < 0 || newer > older {
		t.Errorf("the Stories touching the Topic are not listed newest first:\n%s", res.body)
	}
	if !regexp.MustCompile(`<a href="/stories/\d+">Two billion`).MatchString(res.body) {
		t.Errorf("the Stories do not link their pages:\n%s", res.body)
	}
}

func TestATopicsPageListsOnlyTheStoriesTouchingIt(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)
	// Only the newer Story cites S2, "Scotland will get £2 billion."
	addTopic(t, app, url.Values{"name": {"Scotland"}, "terms": {"Scotland"}})
	// Only the older Story's Editor's Span names the steelworks, and it cites
	// nothing that does.
	addTopic(t, app, url.Values{"name": {"Steel"}, "terms": {"steelworks"}})
	// No Story names Wales.
	addTopic(t, app, url.Values{"name": {"Wales"}, "terms": {"Wales"}})

	scotland := topicPage(t, app, "Scotland").body
	steel := topicPage(t, app, "Steel").body
	wales := topicPage(t, app, "Wales").body

	if !strings.Contains(scotland, "Two billion is a down payment") || strings.Contains(scotland, "Motherwell is where") {
		t.Errorf("Scotland's page does not list exactly the Story citing S2:\n%s", scotland)
	}
	if !strings.Contains(steel, "Motherwell is where") || strings.Contains(steel, "Two billion is a down payment") {
		t.Errorf("an Editor's Span's own words do not touch a Topic:\n%s", steel)
	}
	if !strings.Contains(wales, "No Story the Editor made touches this Topic yet.") || strings.Contains(wales, `href="/stories/`) {
		t.Errorf("Wales's page lists a Story:\n%s", wales)
	}
}

func TestTakingUpOrRemovingATopicChangesItsCountsAndListAtOnceAndNothingElse(t *testing.T) {
	store := openStore(t)
	weaveSpeech(t, store, twoOpinions)
	app, _ := newAppWithLog(t, saturday, store)
	name := "Prime Minister&#39;s Office, 10 Downing Street"
	// rest is what the Record and the Stories pages say, apart from Topics.
	rest := func() string {
		made, err := store.StoriesMade(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		waiting, err := store.StoriesToWeave(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%+v %+v %s", made, waiting, get(t, app, "/stories").body)
	}
	before := rest()

	addTopic(t, app, url.Values{"name": {"Scotland"}, "terms": {"Scotland"}})
	taken := get(t, app, "/items").body
	listed := topicPage(t, app, "Scotland").body
	afterTaking := rest()
	if res := send(t, app, http.MethodDelete, "/topics/1", nil); res.status != http.StatusOK {
		t.Fatalf("remove the Topic: status %d", res.status)
	}
	removed := get(t, app, "/items").body

	if line := sourceLine(t, taken, name); !strings.Contains(line, "Stories: 1 touching a Topic, 1 not") {
		t.Errorf("taking up the Topic did not change the counts at once:\n%s", line)
	}
	if !strings.Contains(listed, "Two billion is a down payment") {
		t.Errorf("the Topic's page does not list the Story touching it at once:\n%s", listed)
	}
	if line := sourceLine(t, removed, name); !strings.Contains(line, "Stories: 0 touching a Topic, 2 not") {
		t.Errorf("removing the Topic did not change the counts back at once:\n%s", line)
	}
	if after := rest(); afterTaking != before || after != before {
		t.Errorf("taking up or removing a Topic changed something else:\nbefore %s\ntaking %s\nafter  %s", before, afterTaking, after)
	}
}

func TestATopicThatIsNotFollowedHasNoPage(t *testing.T) {
	app := newApp(t, saturday)

	for _, path := range []string{"/topics/99", "/topics/x"} {
		if res := get(t, app, path); res.status != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, res.status)
		}
	}
}
