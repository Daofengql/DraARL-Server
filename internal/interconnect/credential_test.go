package interconnect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNodeCredentialEmbedsNodeIDAndHashesDeterministically(t *testing.T) {
	nodeID := "edge-test"
	registration, err := NewRegistrationCredential(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	longTerm, err := NewLongTermCredential(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if CredentialNodeID(registration) != nodeID || CredentialNodeID(longTerm) != nodeID {
		t.Fatal("credential did not preserve node identity")
	}
	if HashCredential(registration) == registration || len(HashCredential(registration)) != 64 {
		t.Fatal("credential hash is invalid")
	}
}

func TestEdgeIdentityOverridesBootstrapButKeepsRotationFallback(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity.json")
	oldCredential, _ := NewLongTermCredential("edge-test")
	if err := SaveEdgeIdentity(identityPath, EdgeIdentity{NodeID: "edge-test", Credential: oldCredential, CredentialEpoch: 1}); err != nil {
		t.Fatal(err)
	}
	newRegistration, _ := NewRegistrationCredential("edge-test")
	configPath := filepath.Join(dir, "config.yaml")
	config := []byte("Edge:\n  Center: 127.0.0.1:60100\n  CenterUDP: 127.0.0.1:60050\n  Token: " + newRegistration + "\n  IdentityFile: identity.json\n")
	if err := os.WriteFile(configPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadEdgeConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Edge.Token != oldCredential {
		t.Fatal("stored identity was not preferred for normal restart")
	}
	nodeID, fallback, ok := cfg.RegistrationFallback()
	if !ok || nodeID != "edge-test" || fallback != newRegistration {
		t.Fatal("new registration token was not retained as rotation fallback")
	}
}
