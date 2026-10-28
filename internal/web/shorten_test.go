package web

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAFactLabellingFeedbackIsShortenedAtAWordOnlyWhenLong(t *testing.T) {
	short := "Andy Burnham spoke in Motherwell."
	if got := shorten(short); got != short {
		t.Errorf("shorten(%q) = %q, want it unchanged", short, got)
	}

	long := "The Chancellor confirmed that the £2.4 billion fund for regional transport schemes will be spent over five years."
	got := shorten(long)
	if !strings.HasSuffix(got, "…") || utf8.RuneCountInString(got) > labelLength+1 {
		t.Fatalf("shorten(long) = %q, want at most %d runes ending in an ellipsis", got, labelLength+1)
	}
	if kept := strings.TrimSuffix(got, "…"); !strings.HasPrefix(long, kept+" ") {
		t.Errorf("shorten(long) = %q, want it cut at a word", got)
	}
}
