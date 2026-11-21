package wikidatatest_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

func searchFor(t *testing.T, text string) string {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), "GET",
		"/w/api.php?action=wbsearchentities&search="+url.QueryEscape(text), nil)
	return wikidatatest.Name(r)
}

// A search is recorded under a name of one file, whatever its text: a slash
// in a name ("RUC/PSNI") is no directory.
func TestASearchIsRecordedUnderOneFilesName(t *testing.T) {
	got := searchFor(t, "RUC/PSNI now")
	if strings.Contains(got, "/") {
		t.Errorf("Name = %q, a path, not a file's name", got)
	}
	if !strings.HasPrefix(got, "search-RUC%2FPSNI_now") || !strings.HasSuffix(got, ".json") {
		t.Errorf("Name = %q, want it to show the text searched", got)
	}
	if got != wikidatatest.SearchName("RUC/PSNI now") {
		t.Errorf("Name = %q, SearchName = %q: two names for one search", got, wikidatatest.SearchName("RUC/PSNI now"))
	}
}

// Searches for texts that differ only in case, or in a space and an
// underscore, are recorded under names that differ in more than case, so
// neither overwrites the other on a filesystem that ignores case (macOS's).
func TestSearchesDifferingInCaseAreRecordedApart(t *testing.T) {
	for _, pair := range [][2]string{{"UK government", "UK Government"}, {"RUC PSNI", "RUC_PSNI"}} {
		a, b := searchFor(t, pair[0]), searchFor(t, pair[1])
		if strings.EqualFold(a, b) {
			t.Errorf("%q and %q are both recorded as %q and %q, one file where case is ignored", pair[0], pair[1], a, b)
		}
	}
}
