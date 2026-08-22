package handler

import (
	"os"
	"testing"

	appjwt "draarl/pkg/jwt"
)

func TestMain(m *testing.M) {
	if err := appjwt.SetSecret("handler-test-secret-0123456789abcdef0123456789abcdef"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
