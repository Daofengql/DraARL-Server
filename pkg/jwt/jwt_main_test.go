package jwt

import (
	"os"
	"testing"
)

const testJWTSecret = "test-jwt-secret-0123456789abcdef0123456789abcdef"

func TestMain(m *testing.M) {
	if err := SetSecret(testJWTSecret); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
