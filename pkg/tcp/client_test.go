package tcp

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestConnectDoesNotDialAfterStop(t *testing.T) {
	client := NewClient("127.0.0.1", "1", nil)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Connect(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Connect error=%v, want net.ErrClosed", err)
	}
}

func TestConnectAndCloseLeavesClientDisconnected(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	client := NewClient("127.0.0.1", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port), nil)
	result := make(chan error, 1)
	go func() {
		result <- client.Connect()
	}()

	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadline := time.Now().Add(time.Second)
	for !client.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !client.IsConnected() {
		t.Fatal("client did not install the accepted connection")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Connect error=%v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect did not finish promptly")
	}
	if client.IsConnected() {
		t.Fatal("client remained connected after Stop")
	}
}

func TestReadErrorClosesReaderConnectionWithoutTouchingReplacement(t *testing.T) {
	readerConn, peerConn := net.Pipe()
	defer peerConn.Close()
	client := NewClient("unused", "0", nil)
	client.mu.Lock()
	client.conn = readerConn
	client.connected = true
	client.mu.Unlock()
	errorsSeen := make(chan error, 1)
	client.SetOnError(func(err error) { errorsSeen <- err })
	done := make(chan struct{})
	go func() {
		client.readMessages(readerConn)
		close(done)
	}()
	_ = peerConn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not stop after peer close")
	}
	if client.IsConnected() {
		t.Fatal("client remained connected after reader error")
	}
	select {
	case <-errorsSeen:
	case <-time.After(time.Second):
		t.Fatal("read error callback was not invoked")
	}
	if err := readerConn.SetReadDeadline(time.Now().Add(time.Millisecond)); err == nil {
		t.Fatal("reader connection remained usable after read error")
	}

	newConn, replacementPeer := net.Pipe()
	defer replacementPeer.Close()
	client.mu.Lock()
	client.conn = newConn
	client.connected = true
	client.mu.Unlock()
	client.handleReadError(readerConn, net.ErrClosed)
	if !client.IsConnected() {
		t.Fatal("stale reader error cleared replacement connection")
	}
	_ = newConn.Close()
}

func TestReadOversizedLineClosesConnection(t *testing.T) {
	readerConn := &scriptedConn{reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, tcpMaxLineSize+1))}
	client := NewClient("unused", "0", nil)
	client.mu.Lock()
	client.conn = readerConn
	client.connected = true
	client.mu.Unlock()
	done := make(chan struct{})
	go func() {
		client.readMessages(readerConn)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("oversized line was not rejected")
	}
	if client.IsConnected() {
		t.Fatal("client remained connected after oversized line")
	}
}

type scriptedConn struct {
	reader *bytes.Reader
}

func (c *scriptedConn) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *scriptedConn) Write([]byte) (int, error)   { return 0, net.ErrClosed }
func (c *scriptedConn) Close() error                { return nil }
func (c *scriptedConn) LocalAddr() net.Addr         { return &net.TCPAddr{} }
func (c *scriptedConn) RemoteAddr() net.Addr        { return &net.TCPAddr{} }
func (c *scriptedConn) SetDeadline(time.Time) error { return nil }
func (c *scriptedConn) SetReadDeadline(time.Time) error {
	return nil
}
func (c *scriptedConn) SetWriteDeadline(time.Time) error {
	return nil
}
