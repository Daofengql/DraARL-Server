package middleware

import (
	"testing"
	"time"

	appjwt "draarl/pkg/jwt"
)

func TestValidateDiscoveryAuthorizationAcceptsOnlyAccessOrDiscoveryTokens(t *testing.T) {
	accessToken, err := appjwt.GenerateToken("web-user", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	discoveryToken, _, err := appjwt.GenerateEdgeDiscoveryToken("device-user", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	claims, tokenUse, err := validateDiscoveryAuthorization("Bearer " + accessToken)
	if err != nil || claims.Username != "web-user" || tokenUse != appjwt.TokenUseAccess {
		t.Fatalf("access token rejected: %#v, %q, %v", claims, tokenUse, err)
	}
	claims, tokenUse, err = validateDiscoveryAuthorization("bearer " + discoveryToken)
	if err != nil || claims.Username != "device-user" || tokenUse != appjwt.TokenUseEdgeDiscovery {
		t.Fatalf("discovery token rejected: %#v, %q, %v", claims, tokenUse, err)
	}
	for _, header := range []string{"", "Basic abc", "Bearer invalid", "Bearer", "Bearer one two"} {
		if _, _, err := validateDiscoveryAuthorization(header); err == nil {
			t.Errorf("invalid authorization %q was accepted", header)
		}
	}
}
