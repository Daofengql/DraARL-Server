package udphub

import (
	"context"
	"errors"
	"slices"
	"strings"
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
	SessionID            uint64
	SessionEpoch         uint64
	DeviceID             int
	OwnerID              int
	Username             string
	CallSign             string
	Nickname             string
	SSID                 byte
	DevModel             byte
	DMRID                uint32
	GroupID              int
	DomainID             uint64
	RxGroupIDs           []int
	RxDomainIDs          []uint64
	GhostSessionID       string
	ClientInstanceID     string
	SessionTag           uint32
	GhostProtocolVersion uint16
	SourceGroupV1        bool
	DisableSend          bool
	DisableRecv          bool
}

type CenterInterconnectHooks struct {
	Activate             func(*CenterLocalSource) error
	ActivateContext      func(context.Context, *CenterLocalSource) error
	Authorize            func(CenterLocalSource) bool
	AcquireVoice         func(CenterLocalSource) bool
	AcquireBroadcast     func(runID uint, domainID uint64, now time.Time) bool
	AcceptBroadcastFrame func(runID uint, domainID uint64, now time.Time) bool
	ReleaseBroadcast     func(runID uint, domainID uint64)
	// RelayPeer sends only an explicitly configured site-to-site mapping.
	RelayPeer func(CenterLocalSource, []byte) error
	Revoke    func(CenterLocalSource)
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
	return hooks.Activate != nil || hooks.ActivateContext != nil
}

func centerSourceFromDevice(dev *models.Device) CenterLocalSource {
	if dev == nil {
		return CenterLocalSource{}
	}
	state := dev.RuntimeSnapshot()
	return CenterLocalSource{
		SessionID: state.InterconnectSessionID, SessionEpoch: state.InterconnectSessionEpoch,
		DeviceID: state.ID, OwnerID: state.OwnerID, Username: state.Username, CallSign: state.CallSign, Nickname: state.Nickname,
		SSID: state.SSID, DevModel: state.DevModel, DMRID: state.DMRID, GroupID: state.GroupID,
		DomainID:   GetActiveCommunicationDomainID(state.GroupID),
		RxGroupIDs: append([]int(nil), state.GhostRxGroupIDs...), RxDomainIDs: GetActiveCommunicationDomainIDs(state.GhostRxGroupIDs),
		GhostSessionID: state.GhostSessionID, ClientInstanceID: state.ClientInstanceID, SessionTag: state.GhostSessionTag,
		GhostProtocolVersion: state.GhostProtocolVersion, SourceGroupV1: state.GhostSessionID != "",
		DisableSend: state.DisableSend, DisableRecv: state.DisableRecv,
	}
}

// ActivateCenterLocalDevice installs or refreshes the centre owner before a
// local device can send. The assigned session is retained on the runtime
// device so every subsequent frame can be checked against the same epoch.
func ActivateCenterLocalDevice(dev *models.Device) error {
	return ActivateCenterLocalDeviceContext(context.Background(), dev)
}

// ActivateCenterLocalDeviceContext is the cancellation-aware form used by
// bounded UDP authentication jobs. Activate remains the compatibility hook
// for existing callers and test integrations.
func ActivateCenterLocalDeviceContext(ctx context.Context, dev *models.Device) error {
	if dev == nil {
		return errors.New("nil centre-local device")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	hooks := centerHooks()
	if hooks.Activate == nil && hooks.ActivateContext == nil {
		return nil
	}
	source := centerSourceFromDevice(dev)
	var err error
	if hooks.ActivateContext != nil {
		err = hooks.ActivateContext(ctx, &source)
	} else {
		err = hooks.Activate(&source)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		if hooks.Revoke != nil {
			hooks.Revoke(source)
		}
		return err
	}
	dev.UpdateRuntime(func(current *models.Device) {
		current.InterconnectSessionID = source.SessionID
		current.InterconnectSessionEpoch = source.SessionEpoch
	})
	return nil
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
	if dev == nil || dev.RuntimeSnapshot().InterconnectSessionID == 0 {
		return false
	}
	source := centerSourceFromDevice(dev)
	if source.DomainID == 0 || source.DisableSend || (hooks.Authorize != nil && !hooks.Authorize(source)) {
		return false
	}
	return hooks.AcquireVoice(source)
}

func RevokeCenterLocalDevice(dev *models.Device) {
	if dev == nil || dev.RuntimeSnapshot().InterconnectSessionID == 0 {
		return
	}
	hooks := centerHooks()
	source := centerSourceFromDevice(dev)
	dev.UpdateRuntime(func(current *models.Device) {
		current.InterconnectSessionID, current.InterconnectSessionEpoch = 0, 0
	})
	if hooks.Revoke != nil {
		hooks.Revoke(source)
	}
}

func RevokeCenterLocalSession(deviceID, ownerID int, ssid byte, sessionID, sessionEpoch uint64) bool {
	revoked := false
	if deviceID > 0 {
		if dev := GetDeviceByID(deviceID); dev != nil {
			state := dev.RuntimeSnapshot()
			if state.InterconnectSessionID == sessionID && state.InterconnectSessionEpoch == sessionEpoch {
				dev.UpdateRuntime(func(current *models.Device) {
					current.InterconnectSessionID, current.InterconnectSessionEpoch = 0, 0
				})
				for _, group := range GetAllGroupsFromCache() {
					removeDeviceConnectionFromGroup(group, dev)
				}
				revoked = true
			}
		}
	}

	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		if GlobalMessageRouter.wsManager.RevokeInterconnectSession(ownerID, ssid, sessionID, sessionEpoch) {
			revoked = true
		}
	}
	for _, ghost := range GlobalUDPGhostManager.GetAll() {
		if ghost == nil {
			continue
		}
		state := ghost.RuntimeSnapshot()
		if state.OwnerID == ownerID && state.SSID == ssid && state.InterconnectSessionID == sessionID && state.InterconnectSessionEpoch == sessionEpoch {
			ghost.UpdateRuntime(func(current *models.Device) {
				current.InterconnectSessionID, current.InterconnectSessionEpoch = 0, 0
			})
			GlobalUDPGhostManager.RemoveSession(state.GhostSessionID)
			ghostsession.Global.Remove(state.GhostSessionID)
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
		RxGroupIDs: source.GetRxGroupIDs(), RxDomainIDs: GetActiveCommunicationDomainIDs(source.GetRxGroupIDs()),
		GhostSessionID: source.GetSessionID(), ClientInstanceID: source.GetClientInstanceID(),
		GhostProtocolVersion: source.GetProtocolVersion(), SourceGroupV1: true,
		DisableSend: source.IsDisabledSend(), DisableRecv: source.IsDisabledRecv(),
	}
}

// RelayCenterLocalWS is intentionally idempotent: a WS sender is activated on
// demand, then the newly assigned owner/epoch is used for the same frame.

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

func GetActiveCommunicationDomainIDs(groupIDs []int) []uint64 {
	set := make(map[uint64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if domainID := GetActiveCommunicationDomainID(groupID); domainID != 0 {
			set[domainID] = struct{}{}
		}
	}
	result := make([]uint64, 0, len(set))
	for domainID := range set {
		result = append(result, domainID)
	}
	slices.Sort(result)
	return result
}

// DeliverCenterPeerAudio injects media from another centre as a stable
// system source. It deliberately rewrites the public DraARL identity to the
// configured Server-* virtual device and never enters ordinary authentication.
func DeliverCenterPeerAudio(groupID int, data []byte, virtualDeviceName, remoteCenterID, linkID, direction string) bool {
	if groupID <= 0 || len(data) == 0 || len(data) > 710 || !strings.HasPrefix(virtualDeviceName, "Server-") || len(virtualDeviceName) > 32 {
		return false
	}
	if group, ok := GetGroupFromCache(groupID); !ok || group == nil || group.Status != 1 || group.IsVirtual {
		return false
	}
	identity := strings.TrimSpace(virtualDeviceName)
	physicalData := protocol.EncodeDraARLv1(identity, "", protocol.SSIDRangeInterconnectMin, protocol.DraARLTypeOpus16K, protocol.DraARLDevModelInterconnect, 0, identity, data)
	if len(physicalData) == 0 {
		return false
	}
	writeUDPDomain(physicalData, getDomainReceiverSnap(groupID), 0, "", 0, "", groupID)
	if GlobalMessageRouter != nil && GlobalMessageRouter.wsManager != nil {
		GlobalMessageRouter.wsManager.BroadcastToGroups(
			voiceDeliveryGroupIDs(groupID), physicalData, 2, interfaces.WSBroadcastFilter{SourceGroupID: groupID},
		)
	}
	MarkAcceptedVoice(groupID, time.Now())
	gid := uint(groupID)
	sender := CommSenderSnapshot{CallSign: identity, Nickname: identity, DevModel: int(protocol.DraARLDevModelInterconnect), SourceType: "intercenter", SourceCenterID: remoteCenterID, LinkID: linkID, VirtualDeviceID: "intercenter:" + remoteCenterID + ":" + linkID + ":" + direction}
	RecordCommPacket(sender.VirtualDeviceID, 0, 255, &gid, nil, sender, data)
	return true
}

func RelayCenterLocalDevice(dev *models.Device, data []byte) error {
	hooks := centerHooks()
	if hooks.RelayPeer == nil {
		return nil
	}
	if dev == nil || len(data) == 0 {
		return errors.New("invalid local relay")
	}
	return hooks.RelayPeer(centerSourceFromDevice(dev), data)
}

func RelayCenterLocalWS(source interfaces.WSDeviceInterface, groupID int, data []byte) error {
	hooks := centerHooks()
	if hooks.RelayPeer == nil {
		return nil
	}
	local := centerSourceFromWS(source, groupID)
	if local.OwnerID <= 0 || local.DomainID == 0 || len(data) == 0 {
		return nil
	}
	return hooks.RelayPeer(local, data)
}
