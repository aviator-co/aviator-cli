package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenEvidence(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("storage got Authorization %q", got)
		}
		_, _ = w.Write([]byte(`{"transcript":[]}`))
	}))
	defer storage.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/verify/evidence/4567" {
			t.Errorf("path = %q", r.URL.Path)
		}
		http.Redirect(w, r, storage.URL+"/signed?sig=1", http.StatusFound)
	}))
	defer srv.Close()

	body, err := newTestClient(srv).OpenEvidence(context.Background(), 4567)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = body.Close() }()
	if data, _ := io.ReadAll(body); string(data) != `{"transcript":[]}` {
		t.Errorf("body = %q", data)
	}
}

func TestOpenEvidenceMissingFromStorage(t *testing.T) {
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
	}))
	defer storage.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+"/signed", http.StatusFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).OpenEvidence(context.Background(), 1)
	if err == nil || err.Error() != "evidence download failed (404)" {
		t.Fatalf("err = %v", err)
	}
}

func TestEvidenceURLNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not-found","message":"Evidence not found."}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).EvidenceURL(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "Evidence not found.") {
		t.Fatalf("err = %v", err)
	}
}
