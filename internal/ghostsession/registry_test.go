package ghostsession

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testRegistration(instanceID string, now time.Time) Registration {
	return Registration{
		ClientInstanceID: instanceID, OwnerID: 7, Username: "alice", CallSign: "BG7AAA",
		DevModel: 101, SSID: 101, Transport: TransportUDP, Now: now,
		Capabilities: []string{CapabilityMultiReceiveV1, CapabilitySourceGroupV1},
		Routing:      Routing{TxGroupID: 1, RxGroupIDs: []int{3, 1, 3}},
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

func TestRegistryRestorePreservesSessionIdentityAndRejectsCollisions(t *testing.T) {
	source := NewRegistry(4, 4)
	session, err := source.Register(testRegistration(uuid.NewString(), time.Now()), Controller{})
	if err != nil {
		t.Fatal(err)
	}
	session.Transport = TransportEdge
	session.Endpoint = "edge-a/recovered"
	session.ProtocolVersion = 1

	restoredRegistry := NewRegistry(4, 4)
	restored, err := restoredRegistry.Restore(session, Controller{})
	if err != nil {
		t.Fatal(err)
	}
	if restored.SessionID != session.SessionID || restored.SessionTag != session.SessionTag {
		t.Fatalf("restored identity changed: %#v", restored)
	}
	if byTag, ok := restoredRegistry.FindByTag(session.SessionTag); !ok || byTag.SessionID != session.SessionID {
		t.Fatal("restored tag index is missing")
	}

	collision := session
	collision.SessionID = uuid.NewString()
	if _, err := restoredRegistry.Restore(collision, Controller{}); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("same instance/tag collision error=%v", err)
	}
	collision = session
	collision.SessionTag++
	if _, err := restoredRegistry.Restore(collision, Controller{}); !errors.Is(err, ErrSessionConflict) {
		t.Fatalf("same session with a different tag error=%v", err)
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
	if _, err := registry.Register(testRegistration(uuid.NewString(), time.Now()), Controller{}); !errors.Is(err, ErrSessionLimit) || !strings.Contains(err.Error(), "active=1 limit=1") {
		t.Fatalf("session limit error=%v", err)
	}
	if _, err := registry.UpdateRouting(session.SessionID, Routing{TxGroupID: 1, RxGroupIDs: []int{1, 2, 3}}); !errors.Is(err, ErrSubscriptionLimit) || !strings.Contains(err.Error(), "requested=3 limit=2") {
		t.Fatalf("subscription limit error=%v", err)
	}
	metrics := registry.Metrics()
	if metrics.OnlineSessions != 1 || metrics.OnlineOwners != 1 {
		t.Fatalf("unexpected registry metrics: %#v", metrics)
	}
	if metrics.SessionLimitRejects != 1 || metrics.SubscriptionLimitRejects != 1 || metrics.Registrations != 1 {
		t.Fatalf("unexpected registry counters: %#v", metrics)
	}
	if metrics.Subscriptions != 2 || metrics.MaxSubscriptionsObserved != 2 || metrics.ByTransport[string(TransportUDP)] != 1 || metrics.ByPlatform[101] != 1 {
		t.Fatalf("unexpected registry distribution: %#v", metrics)
	}
}

func TestRegistryAdminDisconnectIsNotOwnerScoped(t *testing.T) {
	registry := NewRegistry(4, 4)
	disconnected := ""
	session, err := registry.Register(testRegistration(uuid.NewString(), time.Now()), Controller{
		Disconnect: func(reason string) { disconnected = reason },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Disconnect(session.SessionID, "admin_disconnected_session"); err != nil {
		t.Fatal(err)
	}
	if disconnected != "admin_disconnected_session" {
		t.Fatalf("disconnect reason=%q", disconnected)
	}
	if _, ok := registry.Get(session.SessionID); ok {
		t.Fatal("admin-disconnected session remained registered")
	}
	if metrics := registry.Metrics(); metrics.Removals != 1 || metrics.OnlineSessions != 0 {
		t.Fatalf("unexpected removal metrics: %#v", metrics)
	}
}

func TestGlobalRegistryUsesConfiguredLimits(t *testing.T) {
	previous := Global
	t.Cleanup(func() { Global = previous })
	ConfigureGlobal(3, 5)
	if got := MaxSessionsPerOwner(); got != 3 {
		t.Fatalf("configured session limit=%d want=3", got)
	}
	if got := MaxSubscriptions(); got != 5 {
		t.Fatalf("configured subscription limit=%d want=5", got)
	}
}

func TestNormalizeClientInstanceID(t *testing.T) {
	if _, err := NormalizeClientInstanceID(""); !errors.Is(err, ErrInvalidClientInstance) {
		t.Fatalf("empty instance error=%v", err)
	}
	want := uuid.New()
	if id, err := NormalizeClientInstanceID(want.String()); err != nil || id != want.String() {
		t.Fatalf("uuid normalization=(%q,%v)", id, err)
	}
	if _, err := NormalizeClientInstanceID("hardware-id"); !errors.Is(err, ErrInvalidClientInstance) {
		t.Fatalf("invalid instance error=%v", err)
	}
}

func TestRegistryRequiresModernRoutingCapabilities(t *testing.T) {
	registration := testRegistration(uuid.NewString(), time.Now())
	registration.Capabilities = []string{CapabilitySourceGroupV1}
	if _, err := NewRegistry(4, 4).Register(registration, Controller{}); !errors.Is(err, ErrRequiredCapabilities) {
		t.Fatalf("missing capability error=%v", err)
	}
}

func TestStableErrorCode(t *testing.T) {
	for err, want := range map[error]string{
		ErrSessionLimit:      "ghost_session_limit",
		ErrSubscriptionLimit: "subscription_limit",
	} {
		if got := StableErrorCode(fmt.Errorf("wrapped: %w", err)); got != want {
			t.Fatalf("StableErrorCode(%v)=%q want=%q", err, got, want)
		}
	}
}

func TestNormalizeRoutingAlwaysSubscribesTransmitGroup(t *testing.T) {
	routing, err := NormalizeRouting(Routing{TxGroupID: 7, RxGroupIDs: []int{3, 3}}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing.RxGroupIDs) != 2 || routing.RxGroupIDs[0] != 3 || routing.RxGroupIDs[1] != 7 {
		t.Fatalf("normalized routing=%#v", routing)
	}
	if _, err := NormalizeRouting(Routing{TxGroupID: 7, RxGroupIDs: []int{1, 2}}, 2); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("limit after adding transmit group error=%v", err)
	}
}

func TestUpdateRoutingPersistedRollsBackOnRuntimeFailure(t *testing.T) {
	registry := NewRegistry(4, 4)
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
	var persisted []Routing
	_, err = registry.UpdateRoutingPersisted(session.SessionID, Routing{TxGroupID: 9, RxGroupIDs: []int{9}}, func(_ Session, routing Routing) error {
		persisted = append(persisted, routing)
		return nil
	})
	if err == nil {
		t.Fatal("runtime failure was ignored")
	}
	if len(persisted) != 2 || persisted[0].TxGroupID != 9 || persisted[1].TxGroupID != session.TxGroupID {
		t.Fatalf("persistence sequence=%#v", persisted)
	}
	current, _ := registry.Get(session.SessionID)
	if current.TxGroupID != session.TxGroupID {
		t.Fatalf("registry changed after rollback: %#v", current)
	}
}

func TestRegistryConcurrentSessionLifecycle(t *testing.T) {
	const sessionCount = 24
	registry := NewRegistry(sessionCount+1, 4)
	sessions := make([]Session, sessionCount)
	var registerWG sync.WaitGroup
	for i := range sessions {
		registerWG.Add(1)
		go func(index int) {
			defer registerWG.Done()
			registration := testRegistration(uuid.NewString(), time.Now())
			registration.OwnerID = 77
			session, err := registry.Register(registration, Controller{})
			if err != nil {
				t.Errorf("register %d: %v", index, err)
				return
			}
			sessions[index] = session
		}(i)
	}
	registerWG.Wait()
	if got := len(registry.ListOwner(77)); got != sessionCount {
		t.Fatalf("sessions=%d, want %d", got, sessionCount)
	}

	var mutationWG sync.WaitGroup
	for _, session := range sessions {
		session := session
		mutationWG.Add(1)
		go func() {
			defer mutationWG.Done()
			for i := 0; i < 20; i++ {
				registry.UpdateActivity(session.SessionID, "", time.Now())
				groupID := 1 + i%2
				if _, err := registry.UpdateRouting(session.SessionID, Routing{TxGroupID: groupID, RxGroupIDs: []int{groupID, 3}}); err != nil {
					t.Errorf("update %s: %v", session.SessionID, err)
					return
				}
				_, _ = registry.FindByTag(session.SessionTag)
				_ = registry.ListOwner(77)
			}
		}()
	}
	mutationWG.Wait()
}
