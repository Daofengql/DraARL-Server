package udphub

import (
	"errors"
	"sync"
	"time"

	"draarl/internal/ghostsession"
	"draarl/internal/interfaces"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

// CenterLocalSource is the dependency-neutral description of a device whose
// authoritative transport is the centre process. interconnect binds these
// callbacks at runtime; udphub deliberately does not import that package.
type CenterLocalSource struct {
	SessionID    uint64
	SessionEpoch uint64
	DeviceID     int
	OwnerID      int
	Username     string
	CallSign     string
	Nickname     string
	SSID         byte
	DevModel     byte
	DMRID        uint32
	GroupID      int
	DomainID     uint64
	DisableSend  bool
	DisableRecv  bool
}

type CenterInterconnectHooks struct {
	Activate     func(*CenterLocalSource) error
	Authorize    func(CenterLocalSource) bool
	AcquireVoice func(CenterLocalSource) bool
	RemoteOwner  func(ownerID int, ssid byte) bool
	Relay        func(CenterLocalSource, []byte) error
	SendConfig   func(deviceID int, packet []byte, timeout time.Duration) (bool, error)
	Revoke       func(CenterLocalSource)
}

var centerInterconnectBridge struct {
	sync.RWMutex
	hooks CenterInterconnectHooks
}

func SetCenterInterconnectHooks(hooks CenterInterconnectHooks) {
	centerInterconnectBridge.Lock()
	centerInterconnectBridge.hooks = hooks
	centerInterconnectBridge.Unlock()
}

func centerHooks() CenterInterconnectHooks {
	centerInterconnectBridge.RLock()
	hooks := centerInterconnectBridge.hooks
	centerInterconnectBridge.RUnlock()
	return hooks
}

func CenterInterconnectActive() bool {
	hooks := centerHooks()
	return hooks.Activate != nil
}

func centerSourceFromDevice(dev *models.Device) CenterLocalSource {
	if dev == nil {
		return CenterLocalSource{}
	}
	return CenterLocalSource{
		SessionID: dev.InterconnectSessionID, SessionEpoch: dev.InterconnectSessionEpoch,
		DeviceID: dev.ID, OwnerID: dev.OwnerID, Username: dev.Username, CallSign: dev.CallSign, Nickname: dev.Nickname,
		SSID: dev.SSID, DevModel: dev.DevModel, DMRID: dev.DMRID, GroupID: dev.GroupID,
		DomainID:    GetActiveCommunicationDomainID(dev.GroupID),
		DisableSend: dev.DisableSend, DisableRecv: dev.DisableRecv,
	}
}

// ActivateCenterLocalDevice installs or refreshes the centre owner before a
// local device can send. The assigned session is retained on the runtime
// device so every subsequent frame can be checked against the same epoch.
func ActivateCenterLocalDevice(dev *models.Device) error {
	if dev == nil {
		return errors.New("nil centre-local device")
	}
	hooks := centerHooks()
	if hooks.Activate == nil {
		return nil
	}
	source := centerSourceFromDevice(dev)
	if err := hooks.Activate(&source); err != nil {
		return err
	}
	dev.InterconnectSessionID = source.SessionID
	dev.InterconnectSessionEpoch = source.SessionEpoch
	return nil
}

func RelayCenterLocalDevice(dev *models.Device, data []byte) error {
	hooks := centerHooks()
	if hooks.Relay == nil {
		return nil
	}
	if dev == nil || len(data) == 0 {
		return errors.New("invalid centre-local relay")
	}
	if dev.InterconnectSessionID == 0 {
		if err := ActivateCenterLocalDevice(dev); err != nil {
			return err
		}
	}
	return hooks.Relay(centerSourceFromDevice(dev), data)
}

func CenterLocalDeviceAuthoritative(dev *models.Device) bool {
	hooks := centerHooks()
	if hooks.Authorize == nil {
		return true
	}
	return dev != nil && hooks.Authorize(centerSourceFromDevice(dev))
}

func AcquireCenterLocalDeviceVoice(dev *models.Device) bool {
	hooks := centerHooks()
	if hooks.AcquireVoice == nil {
		return true
	}
	if dev == nil || dev.InterconnectSessionID == 0 {
		return false
	}
	source := centerSourceFromDevice(dev)
	if source.DomainID == 0 || source.DisableSend || (hooks.Authorize != nil && !hooks.Authorize(source)) {
		return false
	}
	return hooks.AcquireVoice(source)
}

func RevokeCenterLocalDevice(dev *models.Device) {
	if dev == nil || dev.InterconnectSessionID == 0 {
		return
	}
	hooks := centerHooks()
	source := centerSourceFromDevice(dev)
	dev.InterconnectSessionID, dev.InterconnectSessionEpoch = 0, 0
	if hooks.Revoke != nil {
		hooks.Revoke(source)
	}
}

func RevokeCenterLocalSession(deviceID, ownerID int, ssid byte, sessionID, sessionEpoch uint64) bool {
	revoked := false
	if deviceID > 0 {
		if dev := GetDeviceByID(deviceID); dev != nil && dev.InterconnectSessionID == sessionID && dev.InterconnectSessionEpoch == sessionEpoch {
			dev.InterconnectSessionID, dev.InterconnectSessionEpoch = 0, 0
			for _, group := range GetAllGroupsFromCache() {
				removeDeviceConnectionFromGroup(group, dev)
			}
			revoked = true
		}
	}
	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		if GlobalMessageRouter.wsManager.RevokeInterconnectSession(ownerID, ssid, sessionID, sessionEpoch) {
			revoked = true
		}
	}
	for _, ghost := range GlobalUDPGhostManager.GetAll() {
		if ghost != nil && ghost.OwnerID == ownerID && ghost.SSID == ssid && ghost.InterconnectSessionID == sessionID && ghost.InterconnectSessionEpoch == sessionEpoch {
			ghost.InterconnectSessionID, ghost.InterconnectSessionEpoch = 0, 0
			if ghost.GhostSessionID != "" {
				GlobalUDPGhostManager.RemoveSession(ghost.GhostSessionID)
				ghostsession.Global.Remove(ghost.GhostSessionID)
			} else {
				GlobalUDPGhostManager.Remove(ghost.Username, ghost.SSID)
			}
			revoked = true
		}
	}
	return revoked
}

func centerSourceFromWS(source interfaces.WSDeviceInterface, groupID int) CenterLocalSource {
	if source == nil {
		return CenterLocalSource{}
	}
	sessionID, sessionEpoch := source.GetInterconnectSession()
	return CenterLocalSource{
		SessionID: sessionID, SessionEpoch: sessionEpoch,
		DeviceID: source.GetDeviceID(), OwnerID: source.GetUserID(), Username: source.GetUsername(),
		CallSign: source.GetCallSign(), Nickname: source.GetNickname(), SSID: source.GetSSID(), DevModel: source.GetDevModel(),
		GroupID: groupID, DomainID: GetActiveCommunicationDomainID(groupID),
		DisableSend: source.IsDisabledSend(), DisableRecv: source.IsDisabledRecv(),
	}
}

// RelayCenterLocalWS is intentionally idempotent: a WS sender is activated on
// demand, then the newly assigned owner/epoch is used for the same frame.
func RelayCenterLocalWS(source interfaces.WSDeviceInterface, groupID int, data []byte) error {
	hooks := centerHooks()
	if hooks.Activate == nil || hooks.Relay == nil {
		return nil
	}
	local := centerSourceFromWS(source, groupID)
	if local.OwnerID <= 0 || local.DomainID == 0 || len(data) == 0 {
		return nil
	}
	if err := hooks.Activate(&local); err != nil {
		return err
	}
	source.SetInterconnectSession(local.SessionID, local.SessionEpoch)
	return hooks.Relay(local, data)
}

func AuthorizeCenterLocalWS(source interfaces.WSDeviceInterface, groupID int) bool {
	hooks := centerHooks()
	if hooks.Activate == nil {
		return true
	}
	local := centerSourceFromWS(source, groupID)
	if local.OwnerID <= 0 || local.DomainID == 0 || hooks.Activate(&local) != nil {
		return false
	}
	source.SetInterconnectSession(local.SessionID, local.SessionEpoch)
	return true
}

func AcquireCenterLocalWSVoice(source interfaces.WSDeviceInterface, groupID int) bool {
	hooks := centerHooks()
	if hooks.AcquireVoice == nil {
		return true
	}
	local := centerSourceFromWS(source, groupID)
	if local.SessionID == 0 || local.DomainID == 0 || local.DisableSend || (hooks.Authorize != nil && !hooks.Authorize(local)) {
		return false
	}
	return hooks.AcquireVoice(local)
}

func RevokeCenterLocalWS(source interfaces.WSDeviceInterface) {
	if source == nil {
		return
	}
	sessionID, sessionEpoch := source.GetInterconnectSession()
	if sessionID == 0 {
		return
	}
	source.SetInterconnectSession(0, 0)
	hooks := centerHooks()
	if hooks.Revoke != nil {
		hooks.Revoke(CenterLocalSource{SessionID: sessionID, SessionEpoch: sessionEpoch, OwnerID: source.GetUserID(), SSID: source.GetSSID()})
	}
}

func CenterIdentityOwnedByRemote(ownerID int, ssid byte) bool {
	hooks := centerHooks()
	return hooks.RemoteOwner != nil && hooks.RemoteOwner(ownerID, ssid)
}

func sendRemoteDeviceConfig(deviceID int, packet []byte, timeout time.Duration) (bool, error) {
	hooks := centerHooks()
	if hooks.SendConfig == nil {
		return false, nil
	}
	return hooks.SendConfig(deviceID, packet, timeout)
}

var domainGroupReverseCache sync.Map // domain ID -> representative active group ID

func resetDomainGroupReverseCache() {
	domainGroupReverseCache = sync.Map{}
}

// GetActiveGroupIDForCommunicationDomain resolves an edge frame's opaque
// domain to one local group. A miss scans only on topology/cache changes; the
// realtime path thereafter is a sync.Map lookup.
func GetActiveGroupIDForCommunicationDomain(domainID uint64) int {
	if domainID == 0 {
		return 0
	}
	if value, ok := domainGroupReverseCache.Load(domainID); ok {
		return value.(int)
	}
	for _, group := range GetAllGroupsFromCache() {
		if group == nil || group.Status != 1 || group.IsVirtual {
			continue
		}
		if GetActiveCommunicationDomainID(group.ID) == domainID {
			domainGroupReverseCache.Store(domainID, group.ID)
			return group.ID
		}
	}
	return 0
}

// DeliverInterconnectPacket performs only centre-local UDP/WS fan-out. It
// neither records the frame nor invokes the upstream hook, preventing loops
// and duplicate recordings when an edge frame reaches the centre.
func DeliverInterconnectPacket(domainID uint64, data []byte) bool {
	if protocol.ValidateRelayInnerPacket(data) != nil {
		return false
	}
	groupID := GetActiveGroupIDForCommunicationDomain(domainID)
	if groupID == 0 {
		return false
	}
	writeUDPDomain(data, getDomainReceiverSnap(groupID), 0, "", 0, "", groupID)
	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		GlobalMessageRouter.wsManager.BroadcastToGroups(
			activeDomainGroupIDs(groupID), data, 2, interfaces.WSBroadcastFilter{SourceGroupID: groupID},
		)
	}
	return true
}
