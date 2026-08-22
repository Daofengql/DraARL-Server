package handler

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestUnknownLoginDummyPasswordHashIsValid(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(unknownLoginDummyPasswordHash))
	if err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("dummy bcrypt hash cost=%d err=%v, want valid cost %d", cost, err, bcrypt.DefaultCost)
	}
}
