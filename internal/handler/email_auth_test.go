package handler

import (
	"errors"
	"net/http"
	"testing"

	"draarl/internal/email"
	"draarl/internal/gormdb"
)

func TestCheckVerificationRecipient(t *testing.T) {
	lookupErr := errors.New("database unavailable")
	tests := []struct {
		name       string
		purpose    email.Purpose
		user       *gormdb.User
		lookupErr  error
		wantStatus int
		wantCalls  int
	}{
		{name: "register available", purpose: email.PurposeRegister, wantCalls: 1},
		{name: "register existing", purpose: email.PurposeRegister, user: &gormdb.User{}, wantStatus: http.StatusConflict, wantCalls: 1},
		{name: "login existing", purpose: email.PurposeLogin, user: &gormdb.User{}, wantCalls: 1},
		{name: "login missing", purpose: email.PurposeLogin, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "reset missing", purpose: email.PurposeResetPassword, wantStatus: http.StatusNotFound, wantCalls: 1},
		{name: "database failure", purpose: email.PurposeLogin, lookupErr: lookupErr, wantStatus: http.StatusServiceUnavailable, wantCalls: 1},
		{name: "change email skips lookup", purpose: email.PurposeChangeEmail, wantCalls: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			status, _, err := checkVerificationRecipient(tt.purpose, "user@example.test", func(string) (*gormdb.User, error) {
				calls++
				return tt.user, tt.lookupErr
			})
			if status != tt.wantStatus {
				t.Fatalf("status=%d, want %d", status, tt.wantStatus)
			}
			if calls != tt.wantCalls {
				t.Fatalf("lookup calls=%d, want %d", calls, tt.wantCalls)
			}
			if !errors.Is(err, tt.lookupErr) {
				t.Fatalf("error=%v, want %v", err, tt.lookupErr)
			}
		})
	}
}
