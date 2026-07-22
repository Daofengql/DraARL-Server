package interconnect

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// EdgeConfig is intentionally independent from the centre's database-backed
// configuration.  An edge can therefore boot with only this small YAML file.
type EdgeConfig struct {
	Edge EdgeSettings `yaml:"Edge" json:"edge"`
}
type EdgeSettings struct {
	Center             string `yaml:"Center" json:"center"`
	DataCenter         string `yaml:"DataCenter" json:"data_center"`
	Token              string `yaml:"Token" json:"token"`
	NodeID             string `yaml:"NodeID" json:"node_id"`
	Listen             string `yaml:"Listen" json:"listen"`
	ControlListen      string `yaml:"ControlListen" json:"control_listen"`
	DataListen         string `yaml:"DataListen" json:"data_listen"`
	TLSCAFile          string `yaml:"TLSCAFile" json:"tls_ca_file"`
	TLSServerName      string `yaml:"TLSServerName" json:"tls_server_name"`
	InsecureSkipVerify bool   `yaml:"InsecureSkipVerify" json:"insecure_skip_verify"`
}

func (c *EdgeConfig) SetDefaults() {
	if strings.TrimSpace(c.Edge.Listen) == "" {
		c.Edge.Listen = ":8000"
	}
	if strings.TrimSpace(c.Edge.NodeID) == "" {
		c.Edge.NodeID = randomNodeID()
	}
	if strings.TrimSpace(c.Edge.DataCenter) == "" {
		c.Edge.DataCenter = c.Edge.Center
	}
}
func (c *EdgeConfig) Validate() error {
	c.SetDefaults()
	if strings.TrimSpace(c.Edge.Center) == "" {
		return errors.New("Edge.Center is required")
	}
	if strings.TrimSpace(c.Edge.Token) == "" {
		return errors.New("Edge.Token is required")
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
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
func randomNodeID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "edge-unknown"
	}
	return "edge-" + hex.EncodeToString(b[:])
}
