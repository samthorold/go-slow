// Package wikidatatest serves Wikidata's API from recorded responses, so
// tests fake Wikidata at its boundary, HTTP, and make no network call.
package wikidatatest

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Handler answers a Wikidata API request with the response recorded for it
// in dir, and 404 for one never recorded. A response is recorded under a
// name made from its request:
//
//	search-<text>-<hash>.json wbsearchentities (SearchName)
//	items-<ids>.json          wbgetentities with claims, ids joined by _
//	labels-<ids>.json         wbgetentities with labels only
//
// Each is a response of the real API, fetched once with an honest
// User-Agent and a placeholder contact.
func Handler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := Name(r)
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			http.Error(w, "not recorded: "+name, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(b)
	})
}

// Name is the file a request's response is recorded under.
func Name(r *http.Request) string {
	q := r.URL.Query()
	ids := strings.ReplaceAll(q.Get("ids"), "|", "_")
	switch {
	case q.Get("action") == "wbsearchentities":
		return SearchName(q.Get("search"))
	case q.Get("action") == "wbgetentities" && q.Get("props") == "labels":
		return "labels-" + ids + ".json"
	case q.Get("action") == "wbgetentities":
		return "items-" + ids + ".json"
	}
	return "unknown.json"
}

// SearchName is the file a search for text is recorded under: the text,
// spaces as _ and slashes as %2F, so a reader sees what was searched, then
// the first 8 hex digits of the text's SHA-256, so texts that differ only in
// case ("UK government", "UK Government") or in a space and an underscore
// are two files, even on a filesystem that ignores case (macOS's), where
// one would otherwise overwrite the other.
func SearchName(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "search-" + strings.NewReplacer(" ", "_", "/", "%2F").Replace(text) + "-" + hex.EncodeToString(sum[:4]) + ".json"
}
