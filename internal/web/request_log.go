package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// logRequests logs each request the app answers as one line: its method,
// its path (never the query, nor the form: Feedback can carry what the
// Owner typed), the status answered and how long it took, on the app's
// clock. A static file is logged at debug level, so a page load is one
// info line. A request that
// failed with a 5xx is logged at error level with the error behind it,
// which serverError notes on the request (failed).
func (a *app) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := a.Now()
		rec := &statusRecorder{ResponseWriter: w}
		note := &requestNote{}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), requestNoteKey{}, note)))
		status := rec.status()
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", float64(a.Now().Sub(start).Microseconds()) / 1000,
		}
		level := slog.LevelInfo
		switch {
		case status >= http.StatusInternalServerError:
			level = slog.LevelError
			if note.err != nil {
				attrs = append(attrs, "error", note.err.Error())
			}
		case strings.HasPrefix(r.URL.Path, "/static/"):
			// A page load is one line: the files it loads log at debug.
			level = slog.LevelDebug
		}
		a.Log.Log(r.Context(), level, "request", attrs...)
	})
}

// requestNote is what a handler notes on its request for the request's
// log line.
type requestNote struct {
	err error
}

type requestNoteKey struct{}

// failed notes on the request the error it failed with, for its log line.
func failed(r *http.Request, err error) {
	if note, ok := r.Context().Value(requestNoteKey{}).(*requestNote); ok {
		note.err = err
	}
}

// statusRecorder notes the status a handler answers with.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.code == 0 {
		s.code = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// status is what the handler answered: 200 if it wrote nothing at all.
func (s *statusRecorder) status() int {
	if s.code == 0 {
		return http.StatusOK
	}
	return s.code
}
