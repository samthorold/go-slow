package server_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/samthorold/go-slow/internal/server"
)

func TestStoppingFinishesInFlightRequestsAndRefusesNewOnes(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "finished")
	})
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, stop := context.WithCancel(t.Context())
	ran := make(chan error, 1)
	go func() { ran <- server.Run(ctx, ln, h, 5*time.Second) }()

	type result struct {
		body string
		err  error
	}
	inFlight := make(chan result, 1)
	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/", nil)
		if err != nil {
			inFlight <- result{err: err}
			return
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			inFlight <- result{err: err}
			return
		}
		defer func() { _ = res.Body.Close() }()
		b, err := io.ReadAll(res.Body)
		inFlight <- result{string(b), err}
	}()
	<-entered

	stop() // what SIGTERM does in main

	// New connections are refused once shutdown starts.
	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := dialer.DialContext(t.Context(), "tcp", addr)
		if err != nil {
			break
		}
		_ = c.Close()
		if time.Now().After(deadline) {
			t.Fatal("still accepting connections after stop")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-ran:
		t.Fatalf("Run returned (%v) before the in-flight request finished", err)
	default:
	}

	close(release)
	got := <-inFlight
	if got.err != nil || got.body != "finished" {
		t.Errorf("in-flight request = (%q, %v), want (\"finished\", nil)", got.body, got.err)
	}
	if err := <-ran; err != nil {
		t.Errorf("Run = %v, want nil after a graceful stop", err)
	}
}
