package main

import (
	"testing"

	"github.com/aviator-co/aviator-cli/internal/api"
)

// Same contract as the verify payload: stable keys, and a criteria count that
// comes from the request, since the create response doesn't echo the criteria.
func TestRunbookCreateJSON(t *testing.T) {
	got := decodeJSON(t, newRunbookCreateJSON(&api.CreateRunbookResponse{
		RunbookNumber: 42,
		URL:           "https://app.aviator.co/r/42",
		Status:        "queued",
	}, 3))
	assertJSONFields(t, got, map[string]any{
		"runbook_number": float64(42),
		"runbook_id":     "r/42",
		"url":            "https://app.aviator.co/r/42",
		"status":         "queued",
		"criteria_count": float64(3),
	})
}
