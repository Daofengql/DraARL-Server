package handler

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestValidateGroupPasswordRequirement(t *testing.T) {
	tests := []struct {
		name      string
		groupType int
		password  string
		wantErr   bool
	}{
		{name: "public without password", groupType: groupTypePublic, wantErr: false},
		{name: "public with password", groupType: groupTypePublic, password: "secret", wantErr: false},
		{name: "private without password", groupType: groupTypePrivate, wantErr: true},
		{name: "private with password", groupType: groupTypePrivate, password: "secret", wantErr: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateGroupPasswordRequirement(test.groupType, test.password); (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestIsBcryptGroupPasswordValidatesHashStructure(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("private-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]bool{
		string(hash):                        true,
		"$2b$10$malformed":                  false,
		"$2b$-prefixed-legacy-plaintext":    false,
		"$2b$10$" + strings.Repeat("!", 53): false,
		"legacy-plaintext":                  false,
		"":                                  false,
	} {
		if got := isBcryptGroupPassword(value); got != want {
			t.Fatalf("isBcryptGroupPassword(%q)=%t, want %t", value, got, want)
		}
	}
}
