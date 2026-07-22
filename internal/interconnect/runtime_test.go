package interconnect

import (
	"crypto/tls"
	"testing"
	"time"
)

func TestCenterAndEdgeRuntimeConnectWithoutExternalDependencies(t *testing.T) {
	serverTLS, roots, err := NewSelfSignedTLSConfig("localhost")
	if err != nil {
		t.Fatal(err)
	}
	center, err := StartCenterRuntime(CenterRuntimeConfig{
		ControlListen: "127.0.0.1:0", DataListen: "127.0.0.1:0", TLSConfig: serverTLS,
		ValidateToken: func(nodeID, token string) bool { return nodeID == "edge-test" && token == "token" },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer center.Close()
	clientTLS := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS13}
	edge, err := StartEdgeRuntime(EdgeRuntimeConfig{NodeID: "edge-test", Token: "token", CenterControl: center.Control.Addr().String(), CenterData: center.Data.Addr().String(), Listen: "127.0.0.1:0", TLSConfig: clientTLS})
	if err != nil {
		t.Fatal(err)
	}
	defer edge.Close()
	if edge.Gateway.Addr() == nil || edge.Client.Session == nil {
		t.Fatal("edge runtime did not start")
	}
	select {
	case <-edge.Client.Done():
		t.Fatal("edge disconnected immediately")
	case <-time.After(100 * time.Millisecond):
	}
}
