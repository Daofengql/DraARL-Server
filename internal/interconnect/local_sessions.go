package interconnect

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// LocalDevice is an in-process snapshot. It is never serialized or sent to another site.
type LocalDevice struct {
	SessionID, SessionEpoch          uint64
	DeviceID, OwnerID                int
	Username, CallSign, Nickname     string
	SSID, DevModel                   byte
	DMRID                            uint32
	GroupID                          int
	DomainID                         uint64
	RxGroupIDs                       []int
	RxDomainIDs                      []uint64
	GhostSessionID, ClientInstanceID string
	SessionTag                       uint32
	GhostProtocolVersion             uint16
	SourceGroupV1                    bool
	DisableSend, DisableRecv         bool
}

type voiceOwner struct {
	key       string
	lastFrame time.Time
}

// LocalSessions coordinates local senders, site peers and scheduled broadcasts.
// Device authentication and delivery remain owned by the native UDP/WS services.
type LocalSessions struct {
	mu         sync.Mutex
	next       uint64
	devices    map[uint64]LocalDevice
	identities map[string]uint64
	voices     map[uint64]voiceOwner
	revoke     func(deviceID, ownerID int, ssid byte, sessionID, sessionEpoch uint64)
}

func NewLocalSessions() *LocalSessions {
	return &LocalSessions{devices: make(map[uint64]LocalDevice), identities: make(map[string]uint64), voices: make(map[uint64]voiceOwner)}
}

func localIdentity(d LocalDevice) string {
	if d.GhostSessionID != "" {
		return "ghost:" + d.GhostSessionID
	}
	if d.OwnerID <= 0 {
		return ""
	}
	return fmt.Sprintf("device:%d:%d", d.OwnerID, d.SSID)
}

func voiceKey(d LocalDevice) string { return fmt.Sprintf("local:%d:%d", d.SessionID, d.SessionEpoch) }

func (s *LocalSessions) SetLocalRevocationHandler(fn func(int, int, byte, uint64, uint64)) {
	s.mu.Lock()
	s.revoke = fn
	s.mu.Unlock()
}

func (s *LocalSessions) ActivateLocalDevice(d *LocalDevice) error {
	if d == nil || localIdentity(*d) == "" {
		return errors.New("local device identity is incomplete")
	}
	s.mu.Lock()
	identity := localIdentity(*d)
	old, exists := s.devices[s.identities[identity]]
	var replaced *LocalDevice
	if exists && d.SessionID == old.SessionID && d.SessionEpoch == old.SessionEpoch {
		if old.DomainID != d.DomainID || d.DisableSend {
			s.releaseVoiceLocked(voiceKey(old))
		}
	} else {
		s.next++
		if s.next == 0 {
			s.next++
		}
		d.SessionID, d.SessionEpoch = s.next, 1
		if exists {
			delete(s.devices, old.SessionID)
			s.releaseVoiceLocked(voiceKey(old))
			replaced = &old
		}
	}
	s.devices[d.SessionID] = *d
	s.identities[identity] = d.SessionID
	notify := s.revoke
	s.mu.Unlock()
	if replaced != nil && notify != nil {
		notify(old.DeviceID, old.OwnerID, old.SSID, old.SessionID, old.SessionEpoch)
	}
	return nil
}

func (s *LocalSessions) AuthorizeLocalDevice(d LocalDevice) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.devices[d.SessionID]
	return ok && current.SessionEpoch == d.SessionEpoch && localIdentity(current) == localIdentity(d)
}

func (s *LocalSessions) acquireLocked(domain uint64, key string, now time.Time, currentOnly bool) bool {
	if domain == 0 || key == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	owner, exists := s.voices[domain]
	if exists && now.Sub(owner.lastFrame) > 900*time.Millisecond {
		delete(s.voices, domain)
		exists = false
	}
	if exists && owner.key != key || currentOnly && !exists {
		return false
	}
	s.voices[domain] = voiceOwner{key: key, lastFrame: now}
	return true
}

func (s *LocalSessions) AcquireLocalVoice(d LocalDevice) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.devices[d.SessionID]
	if !ok || current.SessionEpoch != d.SessionEpoch || current.DisableSend || current.DomainID != d.DomainID {
		return false
	}
	return s.acquireLocked(current.DomainID, voiceKey(current), time.Now(), false)
}

func (s *LocalSessions) AcquirePeerVoice(linkID string, domain uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return linkID != "" && s.acquireLocked(domain, "peer:"+linkID, time.Now(), false)
}

func (s *LocalSessions) AcquireScheduledBroadcast(runID uint, domain uint64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return runID != 0 && s.acquireLocked(domain, fmt.Sprintf("broadcast:%d", runID), now, false)
}

func (s *LocalSessions) AcceptScheduledBroadcastFrame(runID uint, domain uint64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return runID != 0 && s.acquireLocked(domain, fmt.Sprintf("broadcast:%d", runID), now, true)
}

func (s *LocalSessions) ReleaseScheduledBroadcast(runID uint, domain uint64) {
	s.mu.Lock()
	if owner, ok := s.voices[domain]; ok && owner.key == fmt.Sprintf("broadcast:%d", runID) {
		delete(s.voices, domain)
	}
	s.mu.Unlock()
}

func (s *LocalSessions) releaseVoiceLocked(key string) {
	for domain, owner := range s.voices {
		if owner.key == key {
			delete(s.voices, domain)
		}
	}
}

func (s *LocalSessions) removeLocked(d LocalDevice) {
	delete(s.devices, d.SessionID)
	if s.identities[localIdentity(d)] == d.SessionID {
		delete(s.identities, localIdentity(d))
	}
	s.releaseVoiceLocked(voiceKey(d))
}

func (s *LocalSessions) RevokeLocalDevice(id, epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok || d.SessionEpoch != epoch {
		return false
	}
	s.removeLocked(d)
	return true
}

func (s *LocalSessions) update(match func(LocalDevice) bool, group int, domain uint64, disableSend, disableRecv bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := false
	for id, d := range s.devices {
		if !match(d) {
			continue
		}
		if d.DomainID != domain || disableSend {
			s.releaseVoiceLocked(voiceKey(d))
		}
		d.GroupID, d.DomainID, d.DisableSend, d.DisableRecv = group, domain, disableSend, disableRecv
		s.devices[id] = d
		updated = true
	}
	return updated, nil
}

func (s *LocalSessions) UpdateActiveDeviceRoute(id, group int, domain uint64, send, recv bool) (bool, error) {
	return s.update(func(d LocalDevice) bool { return id > 0 && d.DeviceID == id }, group, domain, send, recv)
}

func (s *LocalSessions) UpdateActiveIdentityRoute(owner int, ssid byte, group int, domain uint64, send, recv bool) (bool, error) {
	return s.update(func(d LocalDevice) bool { return d.OwnerID == owner && d.SSID == ssid && d.GhostSessionID == "" }, group, domain, send, recv)
}

func (s *LocalSessions) revokeMatching(match func(LocalDevice) bool) int {
	s.mu.Lock()
	var removed []LocalDevice
	for _, d := range s.devices {
		if match(d) {
			removed = append(removed, d)
			s.removeLocked(d)
		}
	}
	notify := s.revoke
	s.mu.Unlock()
	if notify != nil {
		for _, d := range removed {
			notify(d.DeviceID, d.OwnerID, d.SSID, d.SessionID, d.SessionEpoch)
		}
	}
	return len(removed)
}

func (s *LocalSessions) RevokeActiveDevice(id int, _ string) (bool, error) {
	return s.revokeMatching(func(d LocalDevice) bool { return id > 0 && d.DeviceID == id }) > 0, nil
}

func (s *LocalSessions) RevokeActiveOwner(owner int, _ string) (int, error) {
	return s.revokeMatching(func(d LocalDevice) bool { return owner > 0 && d.OwnerID == owner }), nil
}

func (s *LocalSessions) RefreshActiveDeviceDomains(resolve func(int) uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.devices {
		domain := resolve(d.GroupID)
		if d.DomainID != domain {
			s.releaseVoiceLocked(voiceKey(d))
			d.DomainID = domain
			s.devices[id] = d
		}
	}
	return nil
}
