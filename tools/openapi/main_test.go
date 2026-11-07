package main

import (
	"encoding/json"
	"testing"
)

func TestGeneratedContractIncludesProtectedRoutesAndRequests(t *testing.T) {
	data, err := generate("../..")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Paths["/api/devices/{id}"]["put"]["security"] == nil {
		t.Fatal("device mutation lost authentication documentation")
	}
	if doc.Paths["/metrics"]["get"]["x-required-role"] != "admin" {
		t.Fatal("metrics admin requirement missing")
	}
	if doc.Paths["/api/auth/login"]["post"]["requestBody"] == nil {
		t.Fatal("login request schema missing")
	}
	if doc.Paths["/api/radio/sessions/{session_id}/routing"]["put"] == nil {
		t.Fatal("registered radio route missing")
	}
}
