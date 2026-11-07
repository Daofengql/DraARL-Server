package udphub

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestEndpointUsesSocketPeerAndUnmodifiedDatagram(t *testing.T) {
	type received struct {
		data         []byte
		remote, real string
	}
	packets := make(chan received, 2)
	endpoint, err := NewEdgeEndpoint("127.0.0.1:0", func(data []byte, remote, real *net.UDPAddr) {
		packets <- received{append([]byte(nil), data...), remote.String(), real.String()}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	conn, err := net.DialUDP("udp4", nil, endpoint.Addr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// A retired proxy preamble must never replace the socket peer or be stripped.
	for _, data := range [][]byte{[]byte("ordinary datagram"), append([]byte{13, 10, 13, 10, 0, 13, 10, 81, 85, 73, 84, 10, 33, 18, 0, 12}, make([]byte, 20)...)} {
		if _, err := conn.Write(data); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-packets:
			if !bytes.Equal(got.data, data) || got.remote != conn.LocalAddr().String() || got.real != got.remote {
				t.Fatalf("datagram or endpoint changed: %+v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("UDP receive timed out")
		}
	}
}
