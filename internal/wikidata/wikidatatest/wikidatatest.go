// Package wikidatatest serves Wikidata's API from recorded responses, so
// tests fake Wikidata at its boundary, HTTP, and make no network call.
package wikidatatest

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Handler answers a Wikidata API request with the response recorded for it
// in dir, and 404 for one never recorded. A response is recorded under a
// name made from its request:
//
//	search-<text>.json        wbsearchentities, spaces as _
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
		return "search-" + strings.ReplaceAll(q.Get("search"), " ", "_") + ".json"
	case q.Get("action") == "wbgetentities" && q.Get("props") == "labels":
		return "labels-" + ids + ".json"
	case q.Get("action") == "wbgetentities":
		return "items-" + ids + ".json"
	}
	return "unknown.json"
}
