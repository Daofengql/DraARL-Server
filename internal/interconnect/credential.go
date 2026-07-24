package interconnect

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	registrationCredentialPrefix = "draarl-enroll"
	nodeCredentialPrefix         = "draarl-node"
)

func NewNodeID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "edge-" + hex.EncodeToString(value[:]), nil
}

func NewRegistrationCredential(nodeID string) (string, error) {
	return newCredential(registrationCredentialPrefix, nodeID)
}

func NewLongTermCredential(nodeID string) (string, error) {
	return newCredential(nodeCredentialPrefix, nodeID)
}

func newCredential(prefix, nodeID string) (string, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || len(nodeID) > NodeIDSize || strings.Contains(nodeID, ".") {
		return "", errors.New("invalid node ID for credential")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return prefix + "." + nodeID + "." + base64.RawURLEncoding.EncodeToString(secret), nil
}

func CredentialNodeID(credential string) string {
	parts := strings.Split(credential, ".")
	if len(parts) != 3 || (parts[0] != registrationCredentialPrefix && parts[0] != nodeCredentialPrefix) {
		return ""
	}
	if parts[1] == "" || len(parts[1]) > NodeIDSize {
		return ""
	}
	if secret, err := base64.RawURLEncoding.DecodeString(parts[2]); err != nil || len(secret) != 32 {
		return ""
	}
	return parts[1]
}

func HashCredential(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}
