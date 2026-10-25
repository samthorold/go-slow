package web_test

import (
	"net/url"
	"strings"
	"testing"
)

const today = "/briefings/2026-10-24/feedback"

func TestWrongOnAFactFromTheBriefingWithdrawsItAndTheBriefingReRendersWithoutIt(t *testing.T) {
	store := openStore(t)
	speechStory(t, store)
	writeBriefing(t, store, saturday, spoke)
	app, _ := newAppWithLog(t, saturday, store)

	before := get(t, app, "/")
	if !strings.Contains(before.body, `hx-post="`+today+`"`) || !strings.Contains(before.body, `hx-confirm=`) {
		t.Fatalf("the Briefing offers no Feedback on its Facts, or wrong asks no confirmation:\n%s", before.body)
	}

	res := send(t, app, "POST", today, url.Values{"kind": {"wrong"}, "level": {"fact"}, "fact": {"1"}})

	if res.status != 303 || res.header.Get("Location") != "/" {
		t.Fatalf("status %d, Location %q, want 303 to /:\n%s", res.status, res.header.Get("Location"), res.body)
	}
	after := get(t, app, "/")
	if strings.Contains(after.body, "The Prime Minister spoke in Motherwell.") {
		t.Errorf("the statement resting on the withdrawn Fact is still shown:\n%s", after.body)
	}
	story := get(t, app, "/stories/1")
	if !strings.Contains(story.body, "Withdrawn") {
		t.Errorf("the Timeline does not mark the withdrawn Development:\n%s", story.body)
	}
}
