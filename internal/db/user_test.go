package db

import (
	"strings"
	"testing"
)

func TestUserSelectQueryUsesStableLegacyColumnList(t *testing.T) {
	query := userSelectQuery("name = ?")
	normalized := strings.Join(strings.Fields(query), " ")
	want := "SELECT id, name, callsign, gird, phone, password, birthday, sex, avatar, address, roles, introduction, alarm_msg, status, update_time, last_login_time, login_err_times, create_time, openid, nickname, pid, last_login_ip, dmrid, mdcid FROM users WHERE name = ?"
	if normalized != want {
		t.Fatalf("query=%q want=%q", normalized, want)
	}
	if strings.Contains(strings.ToUpper(query), "SELECT *") {
		t.Fatalf("legacy user query must not use SELECT *: %s", query)
	}
}

func TestUserSelectQueryOmitsPredicateWhenEmpty(t *testing.T) {
	query := strings.Join(strings.Fields(userSelectQuery("  ")), " ")
	if strings.Contains(query, " WHERE ") {
		t.Fatalf("empty predicate unexpectedly added WHERE: %s", query)
	}
}

func TestAdminInitializationLocksLookupOnSameConnection(t *testing.T) {
	if adminLookupQuery != "SELECT id FROM users WHERE name = ? LIMIT 1 FOR UPDATE" {
		t.Fatalf("admin lookup must lock the key in the initialization transaction: %s", adminLookupQuery)
	}
}

func TestDeserializeRolesHandlesJSONAndMalformedValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty", input: "", want: []string{"user"}},
		{name: "single bracket", input: "[", want: []string{"user"}},
		{name: "empty array", input: "[]", want: []string{"user"}},
		{name: "json roles", input: `["admin", "user"]`, want: []string{"admin", "user"}},
		{name: "legacy roles", input: "admin,user", want: []string{"admin", "user"}},
		{name: "malformed json", input: `["admin",`, want: []string{"user"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deserializeRoles(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("roles=%v want=%v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("roles=%v want=%v", got, tc.want)
				}
			}
		})
	}
}
