package interconnect

import (
	"testing"
	"time"
)

func TestLocalSessionReplacementAndStaleCleanup(t *testing.T) {
	s := NewLocalSessions()
	first := LocalDevice{DeviceID: 1, OwnerID: 7, SSID: 1, GroupID: 1001, DomainID: 11}
	if err := s.ActivateLocalDevice(&first); err != nil {
		t.Fatal(err)
	}
	if !s.AcquireLocalVoice(first) {
		t.Fatal("first session cannot speak")
	}
	callbacks := 0
	s.SetLocalRevocationHandler(func(device, owner int, ssid byte, id, epoch uint64) {
		callbacks++
		if id != first.SessionID || epoch != first.SessionEpoch {
			t.Fatal("wrong session revoked")
		}
		// Callback must run outside the mutex, and must not remove its replacement.
		if s.RevokeLocalDevice(id, epoch) {
			t.Fatal("replaced session still active")
		}
	})
	replacement := LocalDevice{DeviceID: 1, OwnerID: 7, SSID: 1, GroupID: 1001, DomainID: 11}
	if err := s.ActivateLocalDevice(&replacement); err != nil {
		t.Fatal(err)
	}
	if callbacks != 1 || s.AuthorizeLocalDevice(first) || s.AcquireLocalVoice(first) || !s.AuthorizeLocalDevice(replacement) || !s.AcquireLocalVoice(replacement) {
		t.Fatal("replacement did not fence the previous session")
	}
}

func TestGhostSiblingSessionsAndRoutePermissions(t *testing.T) {
	s := NewLocalSessions()
	a := LocalDevice{OwnerID: 7, SSID: 100, GhostSessionID: "a", GroupID: 1001, DomainID: 11}
	b := a
	b.GhostSessionID = "b"
	b.GroupID, b.DomainID = 1002, 22
	for _, d := range []*LocalDevice{&a, &b} {
		if err := s.ActivateLocalDevice(d); err != nil {
			t.Fatal(err)
		}
	}
	if a.SessionID == b.SessionID || !s.AcquireLocalVoice(a) || !s.AcquireLocalVoice(b) {
		t.Fatal("ghost siblings did not coexist")
	}
	if _, err := s.UpdateActiveIdentityRoute(7, 100, 1003, 33, true, false); err != nil {
		t.Fatal(err)
	}
	if !s.AcquireLocalVoice(a) {
		t.Fatal("physical identity update changed a ghost sibling")
	}
	if err := s.RefreshActiveDeviceDomains(func(group int) uint64 { return 99 }); err != nil {
		t.Fatal(err)
	}
	if s.AcquireLocalVoice(a) {
		t.Fatal("old communication domain accepted after relink")
	}
	a.DomainID, b.DomainID = 99, 99
	if !s.AcquireLocalVoice(a) || s.AcquireLocalVoice(b) {
		t.Fatal("linked groups failed to share the speaker arbiter")
	}
	if !s.RevokeLocalDevice(a.SessionID, a.SessionEpoch) || !s.AcquireLocalVoice(b) {
		t.Fatal("one ghost cleanup removed its sibling or retained voice ownership")
	}
	count, err := s.RevokeActiveOwner(7, "disabled")
	if err != nil || count != 1 || s.AuthorizeLocalDevice(b) {
		t.Fatal("owner revocation did not revoke the remaining ghost")
	}
}

func TestLocalPeerAndBroadcastShareArbitration(t *testing.T) {
	s := NewLocalSessions()
	d := LocalDevice{DeviceID: 1, OwnerID: 7, SSID: 1, GroupID: 1001, DomainID: 11}
	if err := s.ActivateLocalDevice(&d); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if !s.AcquireScheduledBroadcast(1, 11, now) || s.AcquireLocalVoice(d) || s.AcquirePeerVoice("peer", 11) {
		t.Fatal("broadcast was interrupted")
	}
	if s.AcceptScheduledBroadcastFrame(2, 11, now) || s.AcceptScheduledBroadcastFrame(1, 22, now) {
		t.Fatal("broadcast frame accepted without its lease")
	}
	s.ReleaseScheduledBroadcast(2, 11)
	if !s.AcceptScheduledBroadcastFrame(1, 11, now.Add(time.Millisecond)) {
		t.Fatal("foreign release removed the lease")
	}
	if s.AcceptScheduledBroadcastFrame(1, 11, now.Add(time.Second)) {
		t.Fatal("expired lease accepted a stale frame")
	}
	if !s.AcquirePeerVoice("peer", 11) || s.AcquireLocalVoice(d) {
		t.Fatal("peer does not share the voice arbiter")
	}
	if _, err := s.UpdateActiveDeviceRoute(1, 1001, 11, true, false); err != nil {
		t.Fatal(err)
	}
	if s.AcquireLocalVoice(d) {
		t.Fatal("disabled sender acquired voice")
	}
}
