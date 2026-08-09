package handler

import (
	"testing"

	gormdb "draarl/internal/gormdb"
)

func TestAuthRoleFallbackContract(t *testing.T) {
	ordinary := &gormdb.User{Roles: ""}
	admin := &gormdb.User{Roles: "admin"}
	operator := &gormdb.User{Roles: "operator"}

	if !hasRoleGORM(ordinary, "user") || hasRoleGORM(ordinary, "admin") {
		t.Fatal("empty role must retain the ordinary-user fallback")
	}
	if getRoleNameFromUser(ordinary) != "user" || getRoleNameFromUser(admin) != "admin" || getRoleNameFromUser(operator) != "user" {
		t.Fatal("user role response fallback changed")
	}
	if getRoleName([]string{"user", "admin"}) != "admin" || getRoleName([]string{"user"}) != "user" {
		t.Fatal("login role selection changed")
	}
}

func TestAuthRequestDTOsKeepValidationSurface(t *testing.T) {
	login := LoginRequest{Username: "alice", Password: "secret"}
	register := RegisterRequest{Username: "alice", Password: "secret", CallSign: "BA1AA", Email: "alice@example.test"}
	if login.Username == "" || register.Email == "" {
		t.Fatal("auth request DTOs lost required fields")
	}
	if (&UpdateUserRequest{}).Name != "" {
		t.Fatal("unexpected user update DTO state")
	}
}
