package ghostsession

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultMaxSessionsPerOwner = 8
	DefaultMaxSubscriptions    = 16
)

type Controller struct {
	ApplyRouting func(Routing) error
	Disconnect   func(reason string)
}

type Registration struct {
	ClientInstanceID string
	OwnerID          int
	Username         string
	CallSign         string
	Nickname         string
	DevModel         uint8
	SSID             uint8
	Transport        Transport
	Endpoint         string
	ProtocolVersion  uint16
	Capabilities     []string
	Routing          Routing
	DisableSend      bool
	DisableRecv      bool
	Now              time.Time
}

type registryEntry struct {
	session    Session
	controller Controller
}

type Registry struct {
	mutationMu               sync.Mutex
	mu                       sync.RWMutex
	sessions                 map[string]*registryEntry
	ownerSessions            map[int]map[string]struct{}
	instanceSessions         map[string]string
	tagSessions              map[uint32]string
	maxOwnerSessions         int
	maxSubscriptions         int
	registrations            atomic.Uint64
	replacements             atomic.Uint64
	removals                 atomic.Uint64
	sessionLimitRejects      atomic.Uint64
	subscriptionLimitRejects atomic.Uint64
	permissionRevocations    atomic.Uint64
}

type MetricsSnapshot struct {
	OnlineSessions             int            `json:"online_sessions"`
	OnlineOwners               int            `json:"online_owners"`
	Subscriptions              int            `json:"subscriptions"`
	MaxSubscriptionsObserved   int            `json:"max_subscriptions_observed"`
	ByTransport                map[string]int `json:"by_transport"`
	ByPlatform                 map[uint8]int  `json:"by_platform"`
	MaxSessionsPerOwner        int            `json:"max_sessions_per_owner"`
	MaxSubscriptionsPerSession int            `json:"max_subscriptions_per_session"`
	Registrations              uint64         `json:"registrations"`
	Replacements               uint64         `json:"replacements"`
	Removals                   uint64         `json:"removals"`
	SessionLimitRejects        uint64         `json:"session_limit_rejects"`
	SubscriptionLimitRejects   uint64         `json:"subscription_limit_rejects"`
	PermissionRevocations      uint64         `json:"permission_revocations"`
}

func NewRegistry(maxOwnerSessions, maxSubscriptions int) *Registry {
	if maxOwnerSessions <= 0 {
		maxOwnerSessions = DefaultMaxSessionsPerOwner
	}
	if maxSubscriptions <= 0 {
		maxSubscriptions = DefaultMaxSubscriptions
	}
	return &Registry{
		sessions: make(map[string]*registryEntry), ownerSessions: make(map[int]map[string]struct{}),
		instanceSessions: make(map[string]string), tagSessions: make(map[uint32]string),
		maxOwnerSessions: maxOwnerSessions, maxSubscriptions: maxSubscriptions,
	}
}

var Global = NewRegistry(DefaultMaxSessionsPerOwner, DefaultMaxSubscriptions)

// ConfigureGlobal replaces the process-wide registry during startup. It is
// intentionally called before any transport accepts a client, so both centre
// and edge processes enforce the same deployment limits.
func ConfigureGlobal(maxOwnerSessions, maxSubscriptions int) {
	Global = NewRegistry(maxOwnerSessions, maxSubscriptions)
}

func ShortID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if len(sessionID) <= 8 {
		return sessionID
	}
	return sessionID[:8]
}

// MaxSubscriptions returns the active registry's receive subscription limit.
// It lets transport and HTTP layers share configured limits without reaching
// into Registry's internal state.
func MaxSubscriptions() int {
	if Global == nil {
		return DefaultMaxSubscriptions
	}
	return Global.maxSubscriptions
}

// MaxSessionsPerOwner returns the active registry's per-owner session limit.
func MaxSessionsPerOwner() int {
	if Global == nil {
		return DefaultMaxSessionsPerOwner
	}
	return Global.maxOwnerSessions
}

func instanceKey(ownerID int, devModel uint8, clientInstanceID string) string {
	return fmt.Sprintf("%d:%d:%s", ownerID, devModel, clientInstanceID)
}

func randomSessionTag(existing map[uint32]string) (uint32, error) {
	var buffer [4]byte
	for attempts := 0; attempts < 16; attempts++ {
		if _, err := rand.Read(buffer[:]); err != nil {
			return 0, err
		}
		tag := binary.BigEndian.Uint32(buffer[:])
		if tag == 0 {
			continue
		}
		if _, used := existing[tag]; !used {
			return tag, nil
		}
	}
	return 0, errorsNewSessionTagCollision()
}

func errorsNewSessionTagCollision() error {
	return fmt.Errorf("could not allocate unique ghost session tag")
}

func (r *Registry) Register(registration Registration, controller Controller) (Session, error) {
	clientInstanceID, err := NormalizeClientInstanceID(registration.ClientInstanceID)
	if err != nil {
		return Session{}, err
	}
	if err := ValidateCapabilities(registration.Capabilities); err != nil {
		return Session{}, err
	}
	routing, err := r.normalizeRouting(registration.Routing)
	if err != nil {
		return Session{}, err
	}
	if registration.OwnerID <= 0 || registration.Transport == "" {
		return Session{}, fmt.Errorf("owner and transport are required")
	}
	now := registration.Now
	if now.IsZero() {
		now = time.Now()
	}

	key := instanceKey(registration.OwnerID, registration.DevModel, clientInstanceID)
	var replaced *registryEntry
	r.mutationMu.Lock()
	r.mu.Lock()
	oldSessionID := r.instanceSessions[key]
	ownerSet := r.ownerSessions[registration.OwnerID]
	ownerCount := len(ownerSet)
	if oldSessionID != "" {
		ownerCount--
	}
	if ownerCount >= r.maxOwnerSessions {
		r.sessionLimitRejects.Add(1)
		r.mu.Unlock()
		r.mutationMu.Unlock()
		return Session{}, fmt.Errorf("%w: active=%d limit=%d", ErrSessionLimit, ownerCount, r.maxOwnerSessions)
	}
	tag, err := randomSessionTag(r.tagSessions)
	if err != nil {
		r.mu.Unlock()
		r.mutationMu.Unlock()
		return Session{}, err
	}
	if oldSessionID != "" {
		replaced = r.removeLocked(oldSessionID)
	}
	session := Session{
		SessionID: uuid.NewString(), SessionTag: tag, ClientInstanceID: clientInstanceID,
		OwnerID: registration.OwnerID, Username: registration.Username, CallSign: registration.CallSign,
		Nickname: registration.Nickname, DevModel: registration.DevModel, SSID: registration.SSID,
		Transport: registration.Transport, Endpoint: registration.Endpoint, ProtocolVersion: registration.ProtocolVersion,
		Capabilities: normalizeCapabilities(registration.Capabilities), CreatedAt: now, LastActivity: now, Connected: true,
		TxGroupID: routing.TxGroupID, RxGroupIDs: routing.RxGroupIDs,
		DisableSend: registration.DisableSend, DisableRecv: registration.DisableRecv,
	}
	entry := &registryEntry{session: session, controller: controller}
	r.sessions[session.SessionID] = entry
	if r.ownerSessions[session.OwnerID] == nil {
		r.ownerSessions[session.OwnerID] = make(map[string]struct{})
	}
	r.ownerSessions[session.OwnerID][session.SessionID] = struct{}{}
	r.instanceSessions[key] = session.SessionID
	r.tagSessions[tag] = session.SessionID
	r.mu.Unlock()
	r.mutationMu.Unlock()

	if replaced != nil && replaced.controller.Disconnect != nil {
		replaced.controller.Disconnect("client_instance_reconnected")
	}
	r.registrations.Add(1)
	if replaced != nil {
		r.replacements.Add(1)
		r.removals.Add(1)
	}
	return cloneSession(session), nil
}

// Restore reinstalls one center-issued Session after a center process restart.
// The caller must authenticate the recovery proof before calling this method;
// Restore only enforces registry identity, limit, and collision rules.
func (r *Registry) Restore(session Session, controller Controller) (Session, error) {
	sessionID := strings.ToLower(strings.TrimSpace(session.SessionID))
	parsedSessionID, err := uuid.Parse(sessionID)
	if err != nil || parsedSessionID == uuid.Nil || parsedSessionID.String() != sessionID || session.SessionTag == 0 {
		return Session{}, ErrSessionConflict
	}
	clientInstanceID, err := NormalizeClientInstanceID(session.ClientInstanceID)
	if err != nil {
		return Session{}, err
	}
	if err := ValidateCapabilities(session.Capabilities); err != nil {
		return Session{}, err
	}
	if session.OwnerID <= 0 || session.Transport == "" || session.SSID == 0 || session.DevModel == 0 || session.ProtocolVersion == 0 {
		return Session{}, ErrSessionConflict
	}
	routing, err := r.normalizeRouting(session.Routing())
	if err != nil {
		return Session{}, err
	}

	session.SessionID = sessionID
	session.ClientInstanceID = clientInstanceID
	session.Capabilities = normalizeCapabilities(session.Capabilities)
	session.TxGroupID = routing.TxGroupID
	session.RxGroupIDs = routing.RxGroupIDs
	session.Connected = true
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	if session.LastActivity.IsZero() {
		session.LastActivity = time.Now()
	}
	key := instanceKey(session.OwnerID, session.DevModel, session.ClientInstanceID)

	r.mutationMu.Lock()
	r.mu.Lock()
	if existing := r.sessions[session.SessionID]; existing != nil {
		matches := existing.session.OwnerID == session.OwnerID && existing.session.DevModel == session.DevModel &&
			existing.session.SSID == session.SSID && existing.session.ClientInstanceID == session.ClientInstanceID &&
			existing.session.SessionTag == session.SessionTag && existing.session.Transport == session.Transport
		if !matches {
			r.mu.Unlock()
			r.mutationMu.Unlock()
			return Session{}, ErrSessionConflict
		}
		result := cloneSession(existing.session)
		r.mu.Unlock()
		r.mutationMu.Unlock()
		return result, nil
	}
	if existingID := r.instanceSessions[key]; existingID != "" || r.tagSessions[session.SessionTag] != "" {
		r.mu.Unlock()
		r.mutationMu.Unlock()
		return Session{}, ErrSessionConflict
	}
	ownerSet := r.ownerSessions[session.OwnerID]
	if len(ownerSet) >= r.maxOwnerSessions {
		r.sessionLimitRejects.Add(1)
		r.mu.Unlock()
		r.mutationMu.Unlock()
		return Session{}, fmt.Errorf("%w: active=%d limit=%d", ErrSessionLimit, len(ownerSet), r.maxOwnerSessions)
	}
	entry := &registryEntry{session: session, controller: controller}
	r.sessions[session.SessionID] = entry
	if ownerSet == nil {
		ownerSet = make(map[string]struct{})
		r.ownerSessions[session.OwnerID] = ownerSet
	}
	ownerSet[session.SessionID] = struct{}{}
	r.instanceSessions[key] = session.SessionID
	r.tagSessions[session.SessionTag] = session.SessionID
	r.mu.Unlock()
	r.mutationMu.Unlock()
	return cloneSession(session), nil
}

func (r *Registry) normalizeRouting(routing Routing) (Routing, error) {
	normalized, err := NormalizeRouting(routing, r.maxSubscriptions)
	if errors.Is(err, ErrSubscriptionLimit) {
		r.subscriptionLimitRejects.Add(1)
	}
	return normalized, err
}

// ValidateRoutingForSession normalizes routing before handlers perform database
// permission checks. UpdateRoutingPersisted repeats the same validation while
// holding the mutation lock.
func (r *Registry) ValidateRoutingForSession(sessionID string, routing Routing) (Routing, error) {
	r.mu.RLock()
	entry := r.sessions[sessionID]
	if entry == nil {
		r.mu.RUnlock()
		return Routing{}, ErrSessionNotFound
	}
	r.mu.RUnlock()
	return r.normalizeRouting(routing)
}

func (r *Registry) removeLocked(sessionID string) *registryEntry {
	entry := r.sessions[sessionID]
	if entry == nil {
		return nil
	}
	delete(r.sessions, sessionID)
	delete(r.tagSessions, entry.session.SessionTag)
	key := instanceKey(entry.session.OwnerID, entry.session.DevModel, entry.session.ClientInstanceID)
	if r.instanceSessions[key] == sessionID {
		delete(r.instanceSessions, key)
	}
	if ownerSet := r.ownerSessions[entry.session.OwnerID]; ownerSet != nil {
		delete(ownerSet, sessionID)
		if len(ownerSet) == 0 {
			delete(r.ownerSessions, entry.session.OwnerID)
		}
	}
	return entry
}

func (r *Registry) Remove(sessionID string) bool {
	r.mutationMu.Lock()
	r.mu.Lock()
	removed := r.removeLocked(sessionID)
	r.mu.Unlock()
	r.mutationMu.Unlock()
	if removed != nil {
		r.removals.Add(1)
	}
	return removed != nil
}

func (r *Registry) Get(sessionID string) (Session, bool) {
	r.mu.RLock()
	entry := r.sessions[sessionID]
	if entry == nil {
		r.mu.RUnlock()
		return Session{}, false
	}
	session := cloneSession(entry.session)
	r.mu.RUnlock()
	return session, true
}

func (r *Registry) FindByTag(tag uint32) (Session, bool) {
	r.mu.RLock()
	entry := r.sessions[r.tagSessions[tag]]
	if entry == nil {
		r.mu.RUnlock()
		return Session{}, false
	}
	session := cloneSession(entry.session)
	r.mu.RUnlock()
	return session, true
}

func (r *Registry) ListOwner(ownerID int) []Session {
	r.mu.RLock()
	ownerSet := r.ownerSessions[ownerID]
	result := make([]Session, 0, len(ownerSet))
	for sessionID := range ownerSet {
		if entry := r.sessions[sessionID]; entry != nil {
			result = append(result, cloneSession(entry.session))
		}
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].SessionID < result[j].SessionID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func (r *Registry) List() []Session {
	r.mu.RLock()
	result := make([]Session, 0, len(r.sessions))
	for _, entry := range r.sessions {
		if entry != nil {
			result = append(result, cloneSession(entry.session))
		}
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].SessionID < result[j].SessionID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func (r *Registry) Metrics() MetricsSnapshot {
	snapshot := MetricsSnapshot{
		ByTransport: make(map[string]int), ByPlatform: make(map[uint8]int),
		MaxSessionsPerOwner: r.maxOwnerSessions, MaxSubscriptionsPerSession: r.maxSubscriptions,
	}
	r.mu.RLock()
	snapshot.OnlineSessions = len(r.sessions)
	snapshot.OnlineOwners = len(r.ownerSessions)
	for _, entry := range r.sessions {
		if entry == nil {
			continue
		}
		session := &entry.session
		snapshot.ByTransport[string(session.Transport)]++
		snapshot.ByPlatform[session.DevModel]++
		subscriptions := len(session.RxGroupIDs)
		snapshot.Subscriptions += subscriptions
		if subscriptions > snapshot.MaxSubscriptionsObserved {
			snapshot.MaxSubscriptionsObserved = subscriptions
		}
	}
	r.mu.RUnlock()
	snapshot.Registrations = r.registrations.Load()
	snapshot.Replacements = r.replacements.Load()
	snapshot.Removals = r.removals.Load()
	snapshot.SessionLimitRejects = r.sessionLimitRejects.Load()
	snapshot.SubscriptionLimitRejects = r.subscriptionLimitRejects.Load()
	snapshot.PermissionRevocations = r.permissionRevocations.Load()
	return snapshot
}

func (r *Registry) ListByGroup(groupID int) []Session {
	if groupID <= 0 {
		return nil
	}
	r.mu.RLock()
	result := make([]Session, 0)
	for _, entry := range r.sessions {
		if entry == nil {
			continue
		}
		for _, subscribedGroupID := range entry.session.RxGroupIDs {
			if subscribedGroupID == groupID {
				result = append(result, cloneSession(entry.session))
				break
			}
		}
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result
}

func (r *Registry) UpdateActivity(sessionID, endpoint string, now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	r.mu.Lock()
	entry := r.sessions[sessionID]
	if entry != nil {
		entry.session.LastActivity = now
		if endpoint != "" {
			entry.session.Endpoint = endpoint
		}
	}
	r.mu.Unlock()
	return entry != nil
}

func (r *Registry) UpdateRouting(sessionID string, routing Routing) (Session, error) {
	return r.UpdateRoutingPersisted(sessionID, routing, nil)
}

// UpdateRoutingPersisted serializes reconnect, disconnect, persistence, the
// transport projection, and the registry snapshot as one logical mutation.
// The callback is invoked again with the previous routing if runtime
// projection fails.
func (r *Registry) UpdateRoutingPersisted(sessionID string, routing Routing, persist func(Session, Routing) error) (Session, error) {
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	r.mu.RLock()
	entry := r.sessions[sessionID]
	if entry == nil {
		r.mu.RUnlock()
		return Session{}, ErrSessionNotFound
	}
	current := cloneSession(entry.session)
	normalized, err := r.normalizeRouting(routing)
	if err != nil {
		r.mu.RUnlock()
		return Session{}, err
	}
	routing = normalized
	previous := current.Routing()
	controller := entry.controller
	r.mu.RUnlock()
	if persist != nil {
		if err := persist(current, routing); err != nil {
			return Session{}, err
		}
	}
	if controller.ApplyRouting != nil {
		if err := controller.ApplyRouting(routing); err != nil {
			var rollbackErr error
			if persist != nil {
				rollbackErr = persist(current, previous)
			}
			return Session{}, errors.Join(err, rollbackErr)
		}
	}
	r.mu.Lock()
	entry = r.sessions[sessionID]
	if entry == nil {
		r.mu.Unlock()
		return Session{}, ErrSessionNotFound
	}
	entry.session.TxGroupID = routing.TxGroupID
	entry.session.RxGroupIDs = append([]int(nil), routing.RxGroupIDs...)
	session := cloneSession(entry.session)
	r.mu.Unlock()
	return session, nil
}

// RefreshRouting resolves the latest authoritative routing while reconnect,
// disconnect, and API routing mutations are excluded. It is intended for the
// authentication handoff before a transport is published to its live index.
func (r *Registry) RefreshRouting(sessionID string, resolve func(Session) (Routing, error)) (Session, error) {
	if resolve == nil {
		return Session{}, errors.New("routing resolver is required")
	}
	r.mutationMu.Lock()
	defer r.mutationMu.Unlock()
	r.mu.RLock()
	entry := r.sessions[sessionID]
	if entry == nil {
		r.mu.RUnlock()
		return Session{}, ErrSessionNotFound
	}
	current := cloneSession(entry.session)
	controller := entry.controller
	r.mu.RUnlock()
	routing, err := resolve(current)
	if err != nil {
		return Session{}, err
	}
	routing, err = r.normalizeRouting(routing)
	if err != nil {
		return Session{}, err
	}
	if controller.ApplyRouting != nil {
		if err := controller.ApplyRouting(routing); err != nil {
			return Session{}, err
		}
	}
	r.mu.Lock()
	entry = r.sessions[sessionID]
	entry.session.TxGroupID = routing.TxGroupID
	entry.session.RxGroupIDs = append([]int(nil), routing.RxGroupIDs...)
	session := cloneSession(entry.session)
	r.mu.Unlock()
	return session, nil
}

func (r *Registry) DisconnectOwned(ownerID int, sessionID, reason string) error {
	return r.disconnect(ownerID, true, sessionID, reason)
}

func (r *Registry) Disconnect(sessionID, reason string) error {
	return r.disconnect(0, false, sessionID, reason)
}

func (r *Registry) disconnect(ownerID int, enforceOwner bool, sessionID, reason string) error {
	r.mutationMu.Lock()
	r.mu.Lock()
	entry := r.sessions[sessionID]
	if entry == nil || (enforceOwner && entry.session.OwnerID != ownerID) {
		r.mu.Unlock()
		r.mutationMu.Unlock()
		return ErrSessionNotFound
	}
	removed := r.removeLocked(sessionID)
	r.mu.Unlock()
	r.mutationMu.Unlock()
	if removed != nil && removed.controller.Disconnect != nil {
		removed.controller.Disconnect(reason)
	}
	if removed != nil {
		r.removals.Add(1)
		if strings.Contains(strings.ToLower(reason), "permission") {
			r.permissionRevocations.Add(1)
		}
	}
	return nil
}

func (r *Registry) UpdateOwnerCallSign(ownerID int, callSign string) {
	r.mu.Lock()
	for sessionID := range r.ownerSessions[ownerID] {
		if entry := r.sessions[sessionID]; entry != nil {
			entry.session.CallSign = callSign
		}
	}
	r.mu.Unlock()
}
