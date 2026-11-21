package editor_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/editor"
	"github.com/samthorold/go-slow/internal/polite"
	"github.com/samthorold/go-slow/internal/sqlite"
	"github.com/samthorold/go-slow/internal/wikidata"
	"github.com/samthorold/go-slow/internal/wikidata/wikidatatest"
)

// Wikidata as weaving's recorded runs see it: the responses Wikidata gave
// every look-up the fixtures' hydrations make (search, items, and an office's
// holders), fetched once from the real API through the Wikidata adapter,
// polite by Wikimedia's rules, and kept under
// testdata/recorded/weaving/inputs/wikidata/, named as wikidatatest names
// them. The runs look up through them, so a name Wikidata knows is its item,
// and one person named two ways (by an office, by a name) is one Entity, as
// in use. Fetching them again calls Wikidata, not a model, and costs nothing,
// but it redraws what weaving is shown, so it makes weaving's recordings
// stale:
//
//	go test -count=1 ./internal/editor -run '^TestRecordedWikidata$' -record-wikidata
var recordWikidataFlag = flag.Bool("record-wikidata", false,
	"fetch from the real Wikidata the responses weaving's recorded runs look up through, and keep them")

// wikidataCommand is how Wikidata's recorded responses are fetched again.
const wikidataCommand = "go test -count=1 ./internal/editor -run '^TestRecordedWikidata$' -record-wikidata"

// wikidataContact is the contact the fetch names in its User-Agent:
// .env.example's placeholder, never a person's.
const wikidataContact = "owner@example.org"

// The recorded runs' Record resolves at least one person the fixtures name
// two ways, by an office alone and by a name, to one Wikidata item. Run with
// -record-wikidata, it first fetches from the real Wikidata every response
// not kept yet (fetchWikidata).
func TestRecordedWikidata(t *testing.T) {
	dir := filepath.Join(recordedDir, "weaving", "inputs", "wikidata")
	if *recordWikidataFlag {
		fetchWikidata(t, dir)
	}
	store, _ := newRecordAt(t, filepath.Join(t.TempDir(), "go-slow.db"), nil)
	frozen := filepath.Join(recordedDir, "weaving", "inputs", "hydration")
	wd := fixturesWikidata(t, dir)
	var keys []editor.LookupKey
	for _, r := range weavingRuns(t) {
		lookUpThrough(t, store, r, frozen, wd)
		keys = append(keys, fixtureKeys(t, store, r)...)
	}
	is, err := store.EntitiesNamed(t.Context(), keys)
	if err != nil {
		t.Fatal(err)
	}
	byItem := map[string][]editor.LookupKey{}
	for _, k := range keys {
		if e := is[k]; e.Kind == "person" && e.WikidataID != "" && !slices.Contains(byItem[e.WikidataID], k) {
			byItem[e.WikidataID] = append(byItem[e.WikidataID], k)
		}
	}
	var resolved []string
	for id, ks := range byItem {
		byOffice := slices.ContainsFunc(ks, namedByOffice)
		byName := slices.ContainsFunc(ks, func(k editor.LookupKey) bool { return !namedByOffice(k) })
		if byOffice && byName {
			resolved = append(resolved, id)
			t.Logf("%s (%s) is named %v", id, is[ks[0]].Name, ks)
		}
	}
	if len(resolved) == 0 {
		t.Errorf("no person named both by an office and by a name is one Wikidata item: %v", byItem)
	}
}

// Each kept search answers the search its name says (its searchinfo), and
// no two kept files' names differ only in case: on a filesystem that ignores
// case (macOS's) one would have overwritten the other, and a replay on one
// that doesn't (CI's Linux) would find a response missing.
func TestEachKeptWikidataResponseIsUnderItsOwnName(t *testing.T) {
	dir := filepath.Join(recordedDir, "weaving", "inputs", "wikidata")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no responses kept")
	}
	seen := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if other, ok := seen[strings.ToLower(name)]; ok {
			t.Errorf("%s and %s differ only in case", other, name)
		}
		seen[strings.ToLower(name)] = name
		if !strings.HasPrefix(name, "search-") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // the test's own testdata
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Searchinfo struct {
				Search string `json:"search"`
			} `json:"searchinfo"`
		}
		if err := json.Unmarshal(b, &res); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := wikidatatest.SearchName(res.Searchinfo.Search); name != want {
			t.Errorf("%s answers a search for %q, kept as %s", name, res.Searchinfo.Search, want)
		}
	}
}

// A response is kept with only the claims the adapter reads: the adapter
// reads the same items from it as from the response as Wikidata gave it,
// office holders, positions and classes included, and it is smaller.
func TestAKeptWikidataResponseReadsAsTheOneGiven(t *testing.T) {
	const name = "items-Q269909_Q94699104_Q24083437_Q47637556_Q140572997.json"
	given, err := os.ReadFile(filepath.Join("..", "wikidata", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	kept, err := keptOf(given)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) >= len(given) {
		t.Errorf("kept %d bytes of %d: nothing dropped", len(kept), len(given))
	}
	if !bytes.HasSuffix(kept, []byte("}\n")) {
		t.Errorf("kept response does not end in one newline: %q", kept[max(0, len(kept)-10):])
	}
	ids := []string{"Q269909", "Q94699104", "Q24083437", "Q47637556", "Q140572997"}
	read := func(body []byte) []editor.WikidataItem {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
		items, err := fixturesWikidata(t, dir).Items(t.Context(), ids)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	want := read(given)
	if !slices.ContainsFunc(want, func(it editor.WikidataItem) bool { return len(it.Positions) > 0 || len(it.Holders) > 0 }) {
		t.Fatal("the response holds no position or holder: it tests nothing")
	}
	if got := read(kept); !reflect.DeepEqual(got, want) {
		t.Errorf("kept response reads as\n%+v\nwant\n%+v", got, want)
	}
}

// namedByOffice is whether the key names its person by the office alone
// ("Scotland’s First Minister"), not by a name of theirs.
func namedByOffice(k editor.LookupKey) bool {
	name := strings.ToLower(strings.TrimPrefix(k.Name, "the "))
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return r == ' ' || r == '’' || r == '\'' }) {
		if w != "s" && !strings.Contains(strings.ToLower(k.Office), w) {
			return false
		}
	}
	return k.Office != ""
}

// fixtureKeys are the keys of every name the run's Items are hydrated with
// in store.
func fixtureKeys(t *testing.T, store *sqlite.Store, r weavingRun) []editor.LookupKey {
	t.Helper()
	var keys []editor.LookupKey
	for _, f := range r.items {
		h, _, err := store.NewestHydration(t.Context(), f.item.SourceID, f.item.EntryID)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, h.Keys(f.item.Published)...)
	}
	return keys
}

// fixturesWikidata is Wikidata answering from the responses kept in dir,
// through the Wikidata adapter over HTTP, never waiting. A request with no
// response kept is answered 404, so its look-up fails (lookUpThrough).
func fixturesWikidata(t *testing.T, dir string) *wikidata.Client {
	t.Helper()
	srv := httptest.NewServer(wikidatatest.Handler(dir))
	t.Cleanup(srv.Close)
	return wikidata.New(wikidata.Options{
		HTTP: srv.Client(), Endpoint: srv.URL, UserAgent: polite.UserAgent(wikidataContact),
		Now: time.Now, Sleep: func(context.Context, time.Duration) error { return nil },
	})
}

// fetchWikidata keeps in dir the real Wikidata's responses to every look-up
// the fixtures' hydrations make that dir does not hold yet, so it resumes a
// stopped fetch, and never moves a response weaving's recordings rest on;
// to fetch every one again, delete dir first. Look-ups are made as in use: one Record
// carrying both runs, every name looked up through the Wikidata adapter,
// polite by Wikimedia's rules (its User-Agent, a request at a time, Gap
// apart, maxlag, backing off), each response kept as it came but for the
// claims the adapter never reads (keptOf), under the name wikidatatest serves
// it by. A look-up that fails fails the fetch.
func fetchWikidata(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	live := wikidata.New(wikidata.Options{
		HTTP:      &http.Client{Timeout: time.Minute, Transport: keeping{dir: dir, t: t}},
		Endpoint:  wikidata.Endpoint,
		UserAgent: polite.UserAgent(wikidataContact),
		Gap:       wikidata.Gap,
		Now:       time.Now,
		Sleep:     polite.Sleep,
	})
	store, _ := newRecordAt(t, filepath.Join(t.TempDir(), "go-slow.db"), nil)
	frozen := filepath.Join(recordedDir, "weaving", "inputs", "hydration")
	for _, r := range weavingRuns(t) {
		lookUpThrough(t, store, r, frozen, live)
	}
}

// keeping is a transport keeping every successful response it carries in
// dir, named as wikidatatest serves it, with a final newline, as the
// repo's hooks leave a file.
type keeping struct {
	dir string
	t   *testing.T
}

func (k keeping) RoundTrip(req *http.Request) (*http.Response, error) {
	if kept, err := os.ReadFile(filepath.Join(k.dir, wikidatatest.Name(req))); err == nil {
		// Kept already: answered from dir, with no call to Wikidata.
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(bytes.NewReader(kept)), ContentLength: int64(len(kept)), Request: req,
		}, nil
	}
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil || res.StatusCode != http.StatusOK {
		return res, err
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}
	res.Body = io.NopCloser(bytes.NewReader(body))
	kept, err := keptOf(body)
	if err != nil {
		k.t.Errorf("%s: %v", req.URL.RawQuery, err)
		return res, nil
	}
	if err := os.WriteFile(filepath.Join(k.dir, wikidatatest.Name(req)), kept, 0o600); err != nil {
		k.t.Error(err)
	}
	return res, nil
}

// readClaims are the only properties the Wikidata adapter reads of an item:
// instance of (P31), position held (P39) and officeholder (P1308).
var readClaims = []string{"P31", "P39", "P1308"}

// keptOf is the response body as it is kept: as Wikidata gave it, but for an
// item's claims, of which only readClaims are kept, since a country's every
// statement runs to megabytes the adapter never reads; with a final newline,
// as the repo's hooks leave a file.
func keptOf(body []byte) ([]byte, error) {
	var res map[string]json.RawMessage
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	if raw, ok := res["entities"]; ok {
		var ents map[string]map[string]json.RawMessage
		if err := json.Unmarshal(raw, &ents); err != nil {
			return nil, err
		}
		for _, e := range ents {
			raw, ok := e["claims"]
			if !ok {
				continue
			}
			var claims map[string]json.RawMessage
			if err := json.Unmarshal(raw, &claims); err != nil {
				return nil, err
			}
			for p := range claims {
				if !slices.Contains(readClaims, p) {
					delete(claims, p)
				}
			}
			b, err := json.Marshal(claims)
			if err != nil {
				return nil, err
			}
			e["claims"] = b
		}
		b, err := json.Marshal(ents)
		if err != nil {
			return nil, err
		}
		res["entities"] = b
	}
	b, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// lookUpThrough collects, cuts and hydrates the run's Items into store, as
// collectCutAndHydrate does, then looks up every name not yet looked up
// through wd. A name left not looked up (Wikidata failed it, or a response
// not recorded) fails the test.
func lookUpThrough(t *testing.T, store *sqlite.Store, r weavingRun, frozen string, wd editor.Wikidata) {
	t.Helper()
	if err := collectCutAndHydrate(t.Context(), store, r, frozen); err != nil {
		t.Fatal(err)
	}
	lu := editor.NewLookups(editor.LookupDeps{Store: store, Wikidata: wd, Log: slog.New(slog.DiscardHandler)})
	if err := lu.LookUpWaiting(t.Context(), r.at); err != nil {
		t.Fatal(err)
	}
	left, err := store.NamesToLookUp(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("%s: %d names not looked up, %v: fetch Wikidata's responses with\n\t%s", r.name, len(left), left, wikidataCommand)
	}
}
