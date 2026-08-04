package ghostsession

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testRegistration(instanceID string, now time.Time) Registration {
	return Registration{
		ClientInstanceID: instanceID, OwnerID: 7, Username: "alice", CallSign: "BG7AAA",
		DevModel: 101, SSID: 101, Transport: TransportUDP, Now: now,
		Routing: Routing{TxGroupID: 1, RxGroupIDs: []int{3, 1, 3}},
	}
}

func TestRegistryAllowsMultipleInstancesAndReplacesOnlySameInstance(t *testing.T) {
	registry := NewRegistry(4, 4)
	firstInstance := uuid.NewString()
	secondInstance := uuid.NewString()
	disconnected := 0
	first, err := registry.Register(testRegistration(firstInstance, time.Unix(1, 0)), Controller{Disconnect: func(string) { disconnected++ }})
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Register(testRegistration(secondInstance, time.Unix(2, 0)), Controller{})
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID == second.SessionID || len(registry.ListOwner(7)) != 2 {
		t.Fatal("separate client instances did not coexist")
	}
	replacement, err := registry.Register(testRegistration(firstInstance, time.Unix(3, 0)), Controller{})
	if err != nil {
		t.Fatal(err)
	}
	if disconnected != 1 || replacement.SessionID == first.SessionID || len(registry.ListOwner(7)) != 2 {
		t.Fatal("same-instance reconnect did not replace exactly one session")
	}
	if registry.Remove(first.SessionID) {
		t.Fatal("delayed cleanup removed a replacement session")
	}
	if _, ok := registry.Get(replacement.SessionID); !ok {
		t.Fatal("replacement session was lost")
	}
}

func TestRegistryPreservesLegacySingleInstanceSlot(t *testing.T) {
	registry := NewRegistry(4, 4)
	if _, err := registry.Register(testRegistration("", time.Now()), Controller{}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(testRegistration("", time.Now()), Controller{}); !errors.Is(err, ErrInstanceAlreadyOnline) {
		t.Fatalf("legacy duplicate error=%v", err)
	}
}

func TestRegistryRoutingIsNormalizedAndControllerFailureIsAtomic(t *testing.T) {
	registry := NewRegistry(4, 3)
	session, err := registry.Register(testRegistration(uuid.NewString(), time.Now()), Controller{
		ApplyRouting: func(routing Routing) error {
			if routing.TxGroupID == 9 {
				return errors.New("runtime rejected")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := registry.UpdateRouting(session.SessionID, Routing{TxGroupID: 8, RxGroupIDs: []int{8, 3, 8}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TxGroupID != 8 || len(updated.RxGroupIDs) != 2 || updated.RxGroupIDs[0] != 3 || updated.RxGroupIDs[1] != 8 {
		t.Fatalf("routing was not normalized: %#v", updated)
	}
	if _, err := registry.UpdateRouting(session.SessionID, Routing{TxGroupID: 9, RxGroupIDs: []int{9}}); err == nil {
		t.Fatal("controller failure was ignored")
	}
	current, _ := registry.Get(session.SessionID)
	if current.TxGroupID != 8 {
		t.Fatalf("failed update changed registry: %#v", current)
	}
}

func TestRegistryEnforcesLimitsAndTagLookup(t *testing.T) {
	registry := NewRegistry(1, 2)
	session, err := registry.Register(testRegistration(uuid.NewString(), time.Now()), Controller{})
	if err != nil {
		t.Fatal(err)
	}
	if byTag, ok := registry.FindByTag(session.SessionTag); !ok || byTag.SessionID != session.SessionID {
		t.Fatal("session tag index did not resolve the session")
	}
	if _, err := registry.Register(testRegistration(uuid.NewString(), time.Now()), Controller{}); !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("session limit error=%v", err)
	}
	if _, err := registry.UpdateRouting(session.SessionID, Routing{TxGroupID: 1, RxGroupIDs: []int{1, 2, 3}}); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("subscription limit error=%v", err)
	}
}

func TestNormalizeClientInstanceID(t *testing.T) {
	if id, legacy, err := NormalizeClientInstanceID(""); err != nil || id != "" || !legacy {
		t.Fatalf("legacy normalization=(%q,%v,%v)", id, legacy, err)
	}
	want := uuid.New()
	if id, legacy, err := NormalizeClientInstanceID(want.String()); err != nil || legacy || id != want.String() {
		t.Fatalf("uuid normalization=(%q,%v,%v)", id, legacy, err)
	}
	if _, _, err := NormalizeClientInstanceID("hardware-id"); !errors.Is(err, ErrInvalidClientInstance) {
		t.Fatalf("invalid instance error=%v", err)
	}
}
