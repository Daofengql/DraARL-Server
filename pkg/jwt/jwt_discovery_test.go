package jwt

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestDiscoveryTokenCannotBeUsedAsAccessTokenOrRefreshed(t *testing.T) {
	token, expiresAt, err := GenerateEdgeDiscoveryToken(1, "radio-user", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateEdgeDiscoveryToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != "radio-user" || claims.TokenUse != TokenUseEdgeDiscovery {
		t.Fatalf("unexpected discovery claims: %#v", claims)
	}
	if time.Until(expiresAt) > time.Minute || time.Until(expiresAt) < 50*time.Second {
		t.Fatalf("unexpected discovery expiry: %s", expiresAt)
	}
	if _, err := ValidateAccessToken(token); err == nil {
		t.Fatal("discovery token was accepted as an access token")
	}
	if _, err := RefreshToken(token); err == nil {
		t.Fatal("discovery token was upgraded through refresh")
	}
}

func TestAccessTokenCannotBeRefreshedStatelessly(t *testing.T) {
	token, err := GenerateTokenForUser(1, "web-user", []string{"user"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed, err := RefreshToken(token); err == nil || refreshed != "" {
		t.Fatalf("stateless refresh succeeded: token=%q err=%v", refreshed, err)
	}
}

func TestAccessTokenCannotBeUsedAsDiscoveryToken(t *testing.T) {
	token, err := GenerateTokenForUser(1, "web-user", []string{"user"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAccessToken(token); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateEdgeDiscoveryToken(token); err == nil {
		t.Fatal("access token was accepted as a discovery-only token")
	}
}

func TestAccessTokenRejectsLegacyUsernameIdentityAndRequiresExpiry(t *testing.T) {
	now := time.Now()
	legacyClaims := Claims{
		Username: "legacy-user",
		Roles:    []string{"user"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "draarl",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
	}
	legacy, err := jwt.NewWithClaims(jwt.SigningMethodHS256, legacyClaims).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAccessToken(legacy); err == nil {
		t.Fatal("legacy username-only token was accepted")
	}

	withoutExpiry := legacyClaims
	withoutExpiry.UserID = 7
	withoutExpiry.SessionVersion = 1
	withoutExpiry.Subject = "7"
	withoutExpiry.TokenUse = TokenUseAccess
	withoutExpiry.ExpiresAt = nil
	invalid, err := jwt.NewWithClaims(jwt.SigningMethodHS256, withoutExpiry).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAccessToken(invalid); err == nil {
		t.Fatal("access token without expiry was accepted")
	}
}

func TestAccessTokenBindsSubjectAndSessionVersion(t *testing.T) {
	token, err := GenerateTokenForUser(42, "renamable", []string{"user"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateAccessToken(token)
	if err != nil || claims.UserID != 42 || claims.Subject != "42" || claims.SessionVersion != 3 {
		t.Fatalf("claims=%#v err=%v", claims, err)
	}
	claims.Subject = "43"
	invalid, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAccessToken(invalid); err == nil {
		t.Fatal("mismatched subject accepted")
	}
}
