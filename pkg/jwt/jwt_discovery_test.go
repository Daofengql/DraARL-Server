package jwt

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestDiscoveryTokenCannotBeUsedAsAccessTokenOrRefreshed(t *testing.T) {
	token, expiresAt, err := GenerateEdgeDiscoveryToken("radio-user", time.Minute)
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

func TestAccessTokenCannotBeUsedAsDiscoveryToken(t *testing.T) {
	token, err := GenerateToken("web-user", []string{"user"})
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

func TestAccessTokenRequiresExpiryButKeepsLegacyTokenUseCompatibility(t *testing.T) {
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
	legacy, err := jwt.NewWithClaims(jwt.SigningMethodHS256, legacyClaims).SignedString(jwtSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAccessToken(legacy); err != nil {
		t.Fatalf("legacy access token without token_use was rejected: %v", err)
	}

	withoutExpiry := legacyClaims
	withoutExpiry.ExpiresAt = nil
	invalid, err := jwt.NewWithClaims(jwt.SigningMethodHS256, withoutExpiry).SignedString(jwtSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAccessToken(invalid); err == nil {
		t.Fatal("access token without expiry was accepted")
	}
}
