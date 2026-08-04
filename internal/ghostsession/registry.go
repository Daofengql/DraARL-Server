package ghostsession

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultMaxSessionsPerOwner = 8
	DefaultMaxSubscriptions    = 8
)

type Controller struct {
	ApplyRouting func(Routing) error
	Disconnect   func(reason string)
}

type Registration struct {
	ClientInstanceID string
	ReplaceExisting  bool
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
	mu               sync.RWMutex
	sessions         map[string]*registryEntry
	ownerSessions    map[int]map[string]struct{}
	instanceSessions map[string]string
	tagSessions      map[uint32]string
	maxOwnerSessions int
	maxSubscriptions int
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

func instanceKey(ownerID int, devModel uint8, clientInstanceID string, legacy bool) string {
	if legacy {
		return fmt.Sprintf("%d:%d:legacy", ownerID, devModel)
	}
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
	clientInstanceID, legacy, err := NormalizeClientInstanceID(registration.ClientInstanceID)
	if err != nil {
		return Session{}, err
	}
	routing, err := NormalizeRouting(registration.Routing, r.maxSubscriptions)
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

	key := instanceKey(registration.OwnerID, registration.DevModel, clientInstanceID, legacy)
	var replaced *registryEntry
	r.mu.Lock()
	oldSessionID := r.instanceSessions[key]
	if oldSessionID != "" {
		old := r.sessions[oldSessionID]
		if old != nil && legacy && !registration.ReplaceExisting {
			r.mu.Unlock()
			return Session{}, ErrInstanceAlreadyOnline
		}
	}
	ownerSet := r.ownerSessions[registration.OwnerID]
	ownerCount := len(ownerSet)
	if oldSessionID != "" {
		ownerCount--
	}
	if ownerCount >= r.maxOwnerSessions {
		r.mu.Unlock()
		return Session{}, ErrSessionLimit
	}
	tag, err := randomSessionTag(r.tagSessions)
	if err != nil {
		r.mu.Unlock()
		return Session{}, err
	}
	if oldSessionID != "" {
		replaced = r.removeLocked(oldSessionID)
	}
	session := Session{
		SessionID: uuid.NewString(), SessionTag: tag, ClientInstanceID: clientInstanceID, Legacy: legacy,
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

	if replaced != nil && replaced.controller.Disconnect != nil {
		replaced.controller.Disconnect("client_instance_reconnected")
	}
	return cloneSession(session), nil
}

func (r *Registry) removeLocked(sessionID string) *registryEntry {
	entry := r.sessions[sessionID]
	if entry == nil {
		return nil
	}
	delete(r.sessions, sessionID)
	delete(r.tagSessions, entry.session.SessionTag)
	key := instanceKey(entry.session.OwnerID, entry.session.DevModel, entry.session.ClientInstanceID, entry.session.Legacy)
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
	r.mu.Lock()
	removed := r.removeLocked(sessionID)
	r.mu.Unlock()
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
	routing, err := NormalizeRouting(routing, r.maxSubscriptions)
	if err != nil {
		return Session{}, err
	}
	r.mu.RLock()
	entry := r.sessions[sessionID]
	if entry == nil {
		r.mu.RUnlock()
		return Session{}, ErrSessionNotFound
	}
	controller := entry.controller
	r.mu.RUnlock()
	if controller.ApplyRouting != nil {
		if err := controller.ApplyRouting(routing); err != nil {
			return Session{}, err
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

func (r *Registry) DisconnectOwned(ownerID int, sessionID, reason string) error {
	r.mu.Lock()
	entry := r.sessions[sessionID]
	if entry == nil || entry.session.OwnerID != ownerID {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	removed := r.removeLocked(sessionID)
	r.mu.Unlock()
	if removed != nil && removed.controller.Disconnect != nil {
		removed.controller.Disconnect(reason)
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
