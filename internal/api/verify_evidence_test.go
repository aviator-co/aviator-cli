package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadEvidence(t *testing.T) {
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

	var buf bytes.Buffer
	if err := newTestClient(srv).DownloadEvidence(context.Background(), 4567, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != `{"transcript":[]}` {
		t.Errorf("body = %q", buf.String())
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
