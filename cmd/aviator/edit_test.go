package main

import (
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

func TestEditJSON(t *testing.T) {
	version := 5
	got := decodeJSON(t, newEditJSON(&api.VerifySession{
		RunbookNumber: 123,
		URL:           "https://app.aviator.co/r/123",
		Intent:        "new intent",
		Version:       &version,
	}))
	assertJSONFields(t, got, map[string]any{
		"id":      "r/123",
		"url":     "https://app.aviator.co/r/123",
		"intent":  "new intent",
		"version": float64(5),
	})
}
