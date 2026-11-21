package wikidatatest_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

// A search is recorded under a name of one file, whatever its text: a slash
// in a name ("RUC/PSNI") is no directory.
func TestASearchIsRecordedUnderOneFilesName(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/w/api.php?action=wbsearchentities&search=RUC%2FPSNI+now", nil)
	if got, want := wikidatatest.Name(r), "search-RUC%2FPSNI_now.json"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got := wikidatatest.Name(r); strings.Contains(got, "/") {
		t.Errorf("Name = %q, a path, not a file's name", got)
	}
}
