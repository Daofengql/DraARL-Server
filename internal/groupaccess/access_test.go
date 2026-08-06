package groupaccess

import (
	"testing"

	"draarl/internal/gormdb"
)

func TestReceiveAndTransmitGroupPoliciesAreIndependentEntryPoints(t *testing.T) {
	user := &gormdb.User{ID: 7, Roles: "user"}
	group := &gormdb.Group{ID: 11, Type: TypePrivate, OwerID: 9, Status: 1}
	if CanReceiveGroup(user, group, false) || CanTransmitGroup(user, group, false) {
		t.Fatal("unverified private user was authorized")
	}
	if !CanReceiveGroup(user, group, true) || !CanTransmitGroup(user, group, true) {
		t.Fatal("verified private user was not authorized")
	}
}

func TestRuntimeRoutePoliciesDoNotRequireDatabase(t *testing.T) {
	if !CanReceiveRoute(false, []int{3, 7}, 7) || CanReceiveRoute(false, []int{3}, 7) || CanReceiveRoute(true, []int{7}, 7) {
		t.Fatal("receive route policy mismatch")
	}
	if !CanTransmitRoute(false, 7, 7) || CanTransmitRoute(false, 3, 7) || CanTransmitRoute(true, 7, 7) {
		t.Fatal("transmit route policy mismatch")
	}
}
