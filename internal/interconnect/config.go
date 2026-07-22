package interconnect

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// EdgeConfig is intentionally independent from the centre's database-backed
// configuration.  An edge can therefore boot with only this small YAML file.
type EdgeConfig struct {
	Edge            EdgeSettings `yaml:"Edge" json:"edge"`
	sourcePath      string
	bootstrapToken  string
	bootstrapNodeID string
}
type EdgeSettings struct {
	Center             string `yaml:"Center" json:"center"`
	CenterUDP          string `yaml:"CenterUDP" json:"center_udp"`
	Token              string `yaml:"Token" json:"token"`
	NodeID             string `yaml:"NodeID" json:"node_id"`
	Listen             string `yaml:"Listen" json:"listen"`
	ControlListen      string `yaml:"ControlListen" json:"control_listen"`
	TLSCAFile          string `yaml:"TLSCAFile" json:"tls_ca_file"`
	TLSServerName      string `yaml:"TLSServerName" json:"tls_server_name"`
	InsecureSkipVerify bool   `yaml:"InsecureSkipVerify" json:"insecure_skip_verify"`
	IdentityFile       string `yaml:"IdentityFile" json:"identity_file"`
}

func (c *EdgeConfig) SetDefaults() {
	if strings.TrimSpace(c.Edge.Listen) == "" {
		c.Edge.Listen = ":60050"
	}
	if strings.TrimSpace(c.Edge.CenterUDP) == "" {
		host, _, err := net.SplitHostPort(c.Edge.Center)
		if err == nil && host != "" {
			c.Edge.CenterUDP = net.JoinHostPort(host, "60050")
		}
	}
	if strings.TrimSpace(c.Edge.IdentityFile) == "" {
		dir := filepath.Dir(c.sourcePath)
		if strings.TrimSpace(c.sourcePath) == "" {
			dir = "."
		}
		c.Edge.IdentityFile = filepath.Join(dir, "edge-identity.json")
	} else if !filepath.IsAbs(c.Edge.IdentityFile) && c.sourcePath != "" {
		c.Edge.IdentityFile = filepath.Join(filepath.Dir(c.sourcePath), c.Edge.IdentityFile)
	}
}
func (c *EdgeConfig) Validate() error {
	c.SetDefaults()
	if strings.TrimSpace(c.Edge.Center) == "" {
		return errors.New("Edge.Center is required")
	}
	if strings.TrimSpace(c.Edge.CenterUDP) == "" {
		return errors.New("Edge.CenterUDP is required when it cannot be derived from Edge.Center")
	}
	if strings.TrimSpace(c.Edge.Token) == "" {
		return errors.New("Edge.Token is required")
	}
	if strings.TrimSpace(c.Edge.NodeID) == "" {
		return errors.New("Edge.NodeID is required or must be embedded in Edge.Token")
	}
	if len(c.Edge.NodeID) > NodeIDSize {
		return fmt.Errorf("Edge.NodeID exceeds %d bytes", NodeIDSize)
	}
	return nil
}
func LoadEdgeConfig(path string) (*EdgeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read edge config: %w", err)
	}
	var cfg EdgeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode edge config: %w", err)
	}
	absPath, _ := filepath.Abs(path)
	cfg.sourcePath = absPath
	cfg.bootstrapToken = strings.TrimSpace(cfg.Edge.Token)
	cfg.bootstrapNodeID = strings.TrimSpace(cfg.Edge.NodeID)
	if embedded := CredentialNodeID(cfg.bootstrapToken); embedded != "" {
		cfg.bootstrapNodeID = embedded
	}
	cfg.SetDefaults()
	if err := cfg.applyStoredIdentity(); err != nil {
		return nil, err
	}
	if cfg.Edge.NodeID == "" {
		cfg.Edge.NodeID = CredentialNodeID(cfg.Edge.Token)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *EdgeConfig) RegistrationFallback() (nodeID, token string, ok bool) {
	if c == nil || c.bootstrapToken == "" || c.bootstrapToken == c.Edge.Token {
		return "", "", false
	}
	if CredentialNodeID(c.bootstrapToken) != c.bootstrapNodeID || c.bootstrapNodeID == "" {
		return "", "", false
	}
	return c.bootstrapNodeID, c.bootstrapToken, true
}

type EdgeIdentity struct {
	NodeID          string `json:"node_id"`
	Credential      string `json:"credential"`
	CredentialEpoch uint32 `json:"credential_epoch"`
}

func (c *EdgeConfig) applyStoredIdentity() error {
	data, err := os.ReadFile(c.Edge.IdentityFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read edge identity: %w", err)
	}
	var identity EdgeIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return fmt.Errorf("decode edge identity: %w", err)
	}
	if identity.NodeID == "" || CredentialNodeID(identity.Credential) != identity.NodeID {
		return errors.New("edge identity is invalid")
	}
	c.Edge.NodeID, c.Edge.Token = identity.NodeID, identity.Credential
	return nil
}

func SaveEdgeIdentity(path string, identity EdgeIdentity) error {
	if identity.NodeID == "" || CredentialNodeID(identity.Credential) != identity.NodeID {
		return errors.New("cannot save invalid edge identity")
	}
	data, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		// Windows cannot atomically replace an existing destination with
		// os.Rename. Remove only after the complete temporary file exists.
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			_ = os.Remove(temporary)
			return err
		}
		if retryErr := os.Rename(temporary, path); retryErr != nil {
			_ = os.Remove(temporary)
			return retryErr
		}
	}
	return nil
}
