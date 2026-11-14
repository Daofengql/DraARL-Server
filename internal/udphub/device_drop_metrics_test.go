package udphub

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"draarl/internal/models"
	"draarl/internal/protocol"
)

func TestEndpointRejectsAreCountedAndLogsAreThrottledPerDevice(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	device := &models.Device{Username: fmt.Sprintf("reject-log-test-%d", time.Now().UnixNano()), SSID: 7,
		UDPAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50000}}
	packet := &protocol.DraARLv1Packet{Type: protocol.DraARLTypeOpus16K,
		UDPAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 50001}}
	for i := 0; i < 100; i++ {
		if enforceNormalDeviceEndpointBinding(device, packet) {
			t.Fatal("unexpectedly accepted wrong endpoint")
		}
	}
	if count := strings.Count(output.String(), "device="+device.Username+"-7"); count != 1 {
		t.Fatalf("logs=%d, want 1", count)
	}
	for _, metrics := range GetDeviceDropMetrics() {
		if metrics.Username == device.Username {
			if metrics.EndpointRejects != 100 || metrics.VoiceEndpoint != 100 {
				t.Fatalf("metrics=%+v", metrics)
			}
			return
		}
	}
	t.Fatal("device counters not found")
}

func TestDeviceDropMetricsAreBoundedAndKeepRecentlyRejectedDevices(t *testing.T) {
	for i := 0; i < maxDeviceDropEntries+2; i++ {
		recordDeviceDrop(models.DeviceRuntimeSnapshot{Username: fmt.Sprintf("bounded-drop-%d", i)}, "disabled", true, "", "", protocol.DraARLTypeOpus16K)
	}
	metrics := GetDeviceDropMetrics()
	if len(metrics) != maxDeviceDropEntries {
		t.Fatalf("entries=%d", len(metrics))
	}
	if metrics[0].Username != "bounded-drop-257" || metrics[0].VoiceDisabled != 1 {
		t.Fatalf("newest=%+v", metrics[0])
	}
	for _, entry := range metrics {
		if entry.Username == "bounded-drop-0" {
			t.Fatal("old entry was not evicted")
		}
	}
}
