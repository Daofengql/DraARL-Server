package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"draarl/internal/centerbridge"
	"draarl/internal/config"
	"draarl/internal/gormdb"
	"draarl/internal/interconnect"
	oplog "draarl/internal/log"
	appcrypto "draarl/pkg/crypto"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Serialize persistence and runtime replacement so concurrent edits cannot
// re-enable an older approval after a revocation has committed.
var centerLinkMutation sync.Mutex

var centerPeerNamePattern = regexp.MustCompile(`^Server-[A-Za-z0-9_.-]{1,25}$`)

type interCenterLinkRequest struct {
	LinkID            string `json:"link_id" binding:"required"`
	LocalCenterID     string `json:"local_center_id"`
	RemoteCenterID    string `json:"remote_center_id" binding:"required"`
	RemoteAddress     string `json:"remote_address"`
	TLSServerName     string `json:"tls_server_name"`
	TLSPinSHA256      string `json:"tls_pin_sha256"`
	LocalGroupID      int    `json:"local_group_id" binding:"required"`
	RemoteGroupID     int    `json:"remote_group_id" binding:"required"`
	InitiatorCenterID string `json:"initiator_center_id" binding:"required"`
	Direction         string `json:"direction" binding:"required"`
	Enabled           bool   `json:"enabled"`
	Accepted          bool   `json:"accepted"`
	ForwardAudio      bool   `json:"forward_audio"`
	ForwardText       bool   `json:"forward_text"`
	ForwardBroadcast  bool   `json:"forward_broadcast"`
	VirtualDeviceName string `json:"virtual_device_name" binding:"required"`
	Credential        string `json:"credential"`
	CredentialEpoch   uint32 `json:"credential_epoch"`
	RotateCredential  bool   `json:"rotate_credential"`
	// Local media permissions belong to this center and may be changed without
	// reissuing the invitation. Pointer fields preserve the existing policy when
	// older clients omit them.
	LocalSendAudio    *bool `json:"local_send_audio"`
	LocalReceiveAudio *bool `json:"local_receive_audio"`
}

func parsePositiveID(value string) (int, error) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
func centerID() string {
	if cfg := config.TryGet(); cfg != nil {
		return cfg.Interconnect.CenterID
	}
	return ""
}
func peerLinkView(l *gormdb.InterCenterLink) gin.H {
	var status interconnect.CenterPeerStatus
	if r := interconnect.ActiveCenterRuntime(); r != nil {
		if r.HTTPPeers != nil {
			status = r.HTTPPeers.Status(l.LinkID)
		} else if r.Peers != nil {
			status = r.Peers.Status(l.LinkID)
		}
	}
	return gin.H{"link": l, "runtime": status}
}
func ListInterCenterLinks(c *gin.Context) {
	links, err := gormdb.NewInterCenterLinkRepository().List()
	if err != nil {
		peerError(c, 500, "查询中心互联失败")
		return
	}
	items := make([]gin.H, 0, len(links))
	for _, l := range links {
		items = append(items, peerLinkView(l))
	}
	r := interconnect.ActiveCenterRuntime()
	c.JSON(200, gin.H{"code": 200, "data": gin.H{"items": items, "center_id": centerID(), "runtime_enabled": r != nil}})
}
func peerError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": status, "message": message})
}

// PUT replaces all editable fields. Identity and link ID cannot change.
func preparePeer(req interCenterLinkRequest, old *gormdb.InterCenterLink) (gormdb.InterCenterLink, interconnect.CenterPeerConfig, string, error) {
	l := gormdb.InterCenterLink{LinkID: strings.TrimSpace(req.LinkID), LocalCenterID: centerID(), RemoteCenterID: strings.TrimSpace(req.RemoteCenterID), RemoteAddress: strings.TrimSpace(req.RemoteAddress), TLSServerName: strings.TrimSpace(req.TLSServerName), TLSPinSHA256: strings.ToLower(strings.TrimSpace(req.TLSPinSHA256)), LocalGroupID: req.LocalGroupID, RemoteGroupID: req.RemoteGroupID, InitiatorCenterID: strings.TrimSpace(req.InitiatorCenterID), Direction: req.Direction, Enabled: req.Enabled, Accepted: req.Accepted, ForwardAudio: req.ForwardAudio, ForwardText: req.ForwardText, ForwardBroadcast: req.ForwardBroadcast, VirtualDeviceName: strings.TrimSpace(req.VirtualDeviceName), CredentialEpoch: req.CredentialEpoch}
	fail := func(err error) (gormdb.InterCenterLink, interconnect.CenterPeerConfig, string, error) {
		return l, interconnect.CenterPeerConfig{}, "", err
	}
	if req.LocalCenterID != "" && req.LocalCenterID != l.LocalCenterID {
		return fail(errors.New("本地中心 ID 必须与服务配置一致"))
	}
	if old != nil {
		if l.LinkID != old.LinkID || l.RemoteCenterID != old.RemoteCenterID || l.InitiatorCenterID != old.InitiatorCenterID {
			return fail(errors.New("链路及中心身份不可修改，请新建互联对象"))
		}
		l.ID = old.ID
		l.CredentialHash = old.CredentialHash
		l.CredentialCiphertext = old.CredentialCiphertext
		if l.CredentialEpoch == 0 {
			l.CredentialEpoch = old.CredentialEpoch
		}
		l.LocalSendAudio = old.LocalSendAudio
		l.LocalReceiveAudio = old.LocalReceiveAudio
		if req.LocalSendAudio != nil {
			l.LocalSendAudio = *req.LocalSendAudio
		}
		if req.LocalReceiveAudio != nil {
			l.LocalReceiveAudio = *req.LocalReceiveAudio
		}
	} else if l.CredentialEpoch == 0 {
		l.CredentialEpoch = 1
		if req.LocalSendAudio == nil {
			l.LocalSendAudio = true
		} else {
			l.LocalSendAudio = *req.LocalSendAudio
		}
		if req.LocalReceiveAudio == nil {
			l.LocalReceiveAudio = true
		} else {
			l.LocalReceiveAudio = *req.LocalReceiveAudio
		}
	}
	if err := centerbridge.Policy(l).Validate(); err != nil {
		return fail(err)
	}
	group, err := gormdb.NewGroupRepository().GetGroupByID(l.LocalGroupID)
	if err != nil || !canUseGroupAsLinkTarget(group) && (old == nil || l.Enabled && l.Accepted && l.ForwardAudio || l.LocalGroupID != old.LocalGroupID) {
		return fail(errors.New("本地群组必须是已启用的实体群组"))
	}
	credential := req.Credential
	generated := ""
	if req.RotateCredential || old == nil && credential == "" {
		if l.InitiatorCenterID == l.LocalCenterID {
			return fail(errors.New("发起方必须输入接收方提供的互联凭据"))
		}
		bytes := make([]byte, 32)
		if _, err := rand.Read(bytes); err != nil {
			return fail(err)
		}
		credential = hex.EncodeToString(bytes)
		generated = credential
	}
	if credential != "" {
		if len(credential) < 32 || len(credential) > 256 {
			return fail(errors.New("互联凭据长度应为 32–256 字节"))
		}
		if old != nil && l.CredentialEpoch <= old.CredentialEpoch {
			return fail(errors.New("轮换凭据时必须提高凭据代数，并同步修改对端"))
		}
		sum := sha256.Sum256([]byte(credential))
		l.CredentialHash = hex.EncodeToString(sum[:])
		l.CredentialCiphertext = ""
		if l.InitiatorCenterID == l.LocalCenterID {
			l.CredentialCiphertext, err = appcrypto.Encrypt(credential)
			if err != nil {
				return fail(err)
			}
		}
	} else if old != nil && l.CredentialEpoch != old.CredentialEpoch {
		return fail(errors.New("提高代数时必须同时更新凭据"))
	}
	if l.CredentialHash == "" {
		return fail(errors.New("互联凭据不能为空"))
	}
	pc, err := centerbridge.Config(l)
	if err != nil {
		return fail(err)
	}
	return l, pc, generated, nil
}
func applyPeer(pc interconnect.CenterPeerConfig) {
	if r := interconnect.ActiveCenterRuntime(); r != nil && r.Peers != nil {
		_ = r.Peers.SetConfig(pc)
	}
}
func peerAudit(c *gin.Context, action, link string) {
	oplog.AddLog("中心互联 "+action+": "+link, "inter_center_link_"+action, c.GetInt("user_id"), c.GetString("username"), "", c.ClientIP())
}
func CreateInterCenterLink(c *gin.Context) {
	centerLinkMutation.Lock()
	defer centerLinkMutation.Unlock()
	var req interCenterLinkRequest
	if c.ShouldBindJSON(&req) != nil {
		peerError(c, 400, "互联参数错误")
		return
	}
	l, pc, credential, err := preparePeer(req, nil)
	if err != nil {
		peerError(c, 400, err.Error())
		return
	}
	repo := gormdb.NewInterCenterLinkRepository()
	if _, err := repo.GetByLinkID(l.LinkID); err == nil {
		peerError(c, 409, "互联对象已存在")
		return
	} else if !errors.Is(err, gormdb.ErrInterCenterLinkNotFound) {
		peerError(c, 500, "查询互联对象失败")
		return
	}
	if err := repo.Create(&l); err != nil {
		peerError(c, 500, "保存互联对象失败")
		return
	}
	applyPeer(pc)
	peerAudit(c, "create", l.LinkID)
	data := peerLinkView(&l)
	if credential != "" {
		data["credential"] = credential
	}
	c.JSON(200, gin.H{"code": 200, "message": "互联对象已保存", "data": data})
}
func UpdateInterCenterLink(c *gin.Context) {
	centerLinkMutation.Lock()
	defer centerLinkMutation.Unlock()
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		peerError(c, 400, "无效互联 ID")
		return
	}
	repo := gormdb.NewInterCenterLinkRepository()
	old, err := repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gormdb.ErrInterCenterLinkNotFound) {
			peerError(c, 404, "互联对象不存在")
		} else {
			peerError(c, 500, "查询互联对象失败")
		}
		return
	}
	var req interCenterLinkRequest
	if c.ShouldBindJSON(&req) != nil {
		peerError(c, 400, "互联参数错误")
		return
	}
	l, pc, credential, err := preparePeer(req, old)
	if err != nil {
		peerError(c, 400, err.Error())
		return
	}
	fields := map[string]interface{}{"remote_address": l.RemoteAddress, "tls_server_name": l.TLSServerName, "tls_pin_sha256": l.TLSPinSHA256, "local_group_id": l.LocalGroupID, "remote_group_id": l.RemoteGroupID, "direction": l.Direction, "enabled": l.Enabled, "accepted": l.Accepted, "forward_audio": l.ForwardAudio, "forward_text": false, "forward_broadcast": false, "virtual_device_name": l.VirtualDeviceName, "credential_epoch": l.CredentialEpoch, "credential_hash": l.CredentialHash, "credential_ciphertext": l.CredentialCiphertext, "local_send_audio": l.LocalSendAudio, "local_receive_audio": l.LocalReceiveAudio}
	if err := repo.Update(id, fields); err != nil {
		peerError(c, 500, "更新互联对象失败")
		return
	}
	applyPeer(pc)
	peerAudit(c, "update", l.LinkID)
	saved, _ := repo.GetByID(id)
	if saved == nil {
		saved = &l
	}
	if r := interconnect.ActiveCenterRuntime(); r != nil && r.HTTPPeers != nil {
		r.HTTPPeers.UpdateLinkPolicy(interconnect.CenterPeerLink{
			LinkID: saved.LinkID, LocalCenterID: saved.LocalCenterID, RemoteCenterID: saved.RemoteCenterID,
			LocalGroupID: saved.LocalGroupID, RemoteGroupID: saved.RemoteGroupID, InitiatorCenterID: saved.InitiatorCenterID,
			Direction: saved.Direction, Enabled: saved.Enabled, Accepted: saved.Accepted, ForwardAudio: saved.ForwardAudio,
			VirtualDeviceName: saved.VirtualDeviceName, CredentialEpoch: saved.CredentialEpoch,
		}, saved.LocalSendAudio, saved.LocalReceiveAudio, saved.RemoteSendAudio, saved.RemoteReceiveAudio)
		if saved.Enabled && saved.Accepted && saved.ForwardAudio {
			_ = r.HTTPPeers.Bind(saved.LinkID)
		}
	}
	data := peerLinkView(saved)
	if credential != "" {
		data["credential"] = credential
	}
	c.JSON(200, gin.H{"code": 200, "message": "互联对象已更新", "data": data})
}
func DeleteInterCenterLink(c *gin.Context) {
	centerLinkMutation.Lock()
	defer centerLinkMutation.Unlock()
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		peerError(c, 400, "无效互联 ID")
		return
	}
	repo := gormdb.NewInterCenterLinkRepository()
	l, err := repo.GetByID(id)
	if errors.Is(err, gormdb.ErrInterCenterLinkNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
		peerError(c, 404, "互联对象不存在")
		return
	}
	if err != nil {
		peerError(c, 500, "查询互联对象失败")
		return
	}
	if err := repo.Delete(id); err != nil {
		peerError(c, 500, "删除互联对象失败")
		return
	}
	if r := interconnect.ActiveCenterRuntime(); r != nil && r.Peers != nil {
		r.Peers.RemoveLink(l.LinkID)
	}
	if r := interconnect.ActiveCenterRuntime(); r != nil && r.HTTPPeers != nil {
		r.HTTPPeers.RemoveLink(l.LinkID)
	}
	peerAudit(c, "delete", l.LinkID)
	c.JSON(200, gin.H{"code": 200, "message": "互联对象已删除"})
}

type createPeerInviteRequest struct {
	LocalGroupID      int    `json:"local_group_id" binding:"required"`
	SendAudio         bool   `json:"send_audio"`
	ReceiveAudio      bool   `json:"receive_audio"`
	VirtualDeviceName string `json:"virtual_device_name"`
}

type importPeerInviteRequest struct {
	InviteToken   string `json:"invite_token" binding:"required"`
	RemoteAddress string `json:"remote_address" binding:"required"`
	LocalGroupID  int    `json:"local_group_id" binding:"required"`
	SendAudio     bool   `json:"send_audio"`
	ReceiveAudio  bool   `json:"receive_audio"`
}

type admitPeerInviteRequest struct {
	InviteToken  string `json:"invite_token" binding:"required"`
	CenterID     string `json:"center_id" binding:"required"`
	LocalGroupID int    `json:"local_group_id" binding:"required"`
	SendAudio    bool   `json:"send_audio"`
	ReceiveAudio bool   `json:"receive_audio"`
}

func randomURLToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func centerUDPAddress(base string) string {
	cfg := config.TryGet()
	if cfg == nil {
		return ""
	}
	host := ""
	if u, err := url.Parse(strings.TrimSpace(base)); err == nil {
		host = u.Hostname()
	}
	if host == "" {
		if h, _, err := net.SplitHostPort(strings.TrimSpace(base)); err == nil {
			host = h
		} else {
			host = strings.TrimSpace(base)
		}
	}
	if host == "" {
		host = cfg.Interconnect.CenterID
	}
	port := cfg.System.Port
	// The main UDP listener is configured in System.Port. Access-discovery
	// settings may describe a legacy/default port and must not redirect the
	// freshly admitted session to a different socket.
	if host == "" || port == "" {
		return ""
	}
	return net.JoinHostPort(host, port)
}

func CreateCenterPeerInvite(c *gin.Context) {
	var req createPeerInviteRequest
	if c.ShouldBindJSON(&req) != nil || req.LocalGroupID <= 0 {
		peerError(c, 400, "邀请参数错误")
		return
	}
	group, err := gormdb.NewGroupRepository().GetGroupByID(req.LocalGroupID)
	if err != nil || !canUseGroupAsLinkTarget(group) {
		peerError(c, 400, "本地群组必须是已启用的实体群组")
		return
	}
	cfg := config.TryGet()
	if cfg == nil || strings.TrimSpace(cfg.Interconnect.CenterID) == "" {
		peerError(c, 400, "未配置本地中心 ID")
		return
	}
	token, err := randomURLToken(32)
	if err != nil {
		peerError(c, 500, "生成邀请失败")
		return
	}
	inviteID, err := randomURLToken(16)
	if err != nil {
		peerError(c, 500, "生成邀请失败")
		return
	}
	name := strings.TrimSpace(req.VirtualDeviceName)
	if name == "" {
		name = "Server-" + cfg.Interconnect.CenterID
	}
	if !centerPeerNamePattern.MatchString(name) {
		peerError(c, 400, "虚拟设备名称必须以 Server- 开头且不超过 32 个字符")
		return
	}
	hash := gormdb.HashCenterPeerInviteToken(token)
	invite := &gormdb.CenterPeerInvite{InviteID: inviteID, TokenHash: hash, CenterID: cfg.Interconnect.CenterID, LocalGroupID: req.LocalGroupID, SendAudio: req.SendAudio, ReceiveAudio: req.ReceiveAudio, VirtualDeviceName: name, Enabled: true}
	if err := gormdb.NewCenterPeerInviteRepository().Create(invite); err != nil {
		peerError(c, 500, "保存邀请失败")
		return
	}
	peerAudit(c, "invite_create", inviteID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "邀请已生成，请只向对端展示一次", "data": gin.H{"invite_id": inviteID, "invite_token": token, "center_id": invite.CenterID, "local_group_id": invite.LocalGroupID, "send_audio": invite.SendAudio, "receive_audio": invite.ReceiveAudio, "virtual_device_name": invite.VirtualDeviceName}})
}

func admitPeerInvite(c *gin.Context) {
	centerLinkMutation.Lock()
	defer centerLinkMutation.Unlock()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var req admitPeerInviteRequest
	if c.ShouldBindJSON(&req) != nil || req.LocalGroupID <= 0 || strings.TrimSpace(req.CenterID) == "" {
		peerError(c, 400, "准入参数错误")
		return
	}
	repo := gormdb.NewCenterPeerInviteRepository()
	invite, err := repo.GetByTokenHash(gormdb.HashCenterPeerInviteToken(req.InviteToken))
	if err != nil || invite == nil || !invite.Enabled {
		peerError(c, 401, "邀请 Token 无效或已停用")
		return
	}
	if invite.Bound && (invite.BoundCenterID != req.CenterID || invite.BoundGroupID != req.LocalGroupID) {
		peerError(c, 409, "邀请已经绑定到另一端")
		return
	}
	linkRepo := gormdb.NewInterCenterLinkRepository()
	// A lost response is common on an HTTP control path. Once a token has
	// already been bound to the same centre/group, return the original session
	// instead of rotating the key or creating a second link on every retry.
	if invite.Bound {
		if strings.TrimSpace(invite.LinkID) == "" {
			peerError(c, 409, "邀请绑定状态不完整，请重新生成邀请")
			return
		}
		existing, getErr := linkRepo.GetByLinkID(invite.LinkID)
		if getErr != nil || existing == nil || existing.SessionID == "" || existing.SessionKeyCiphertext == "" {
			peerError(c, 409, "邀请绑定状态不完整，请重新生成邀请")
			return
		}
		key, keyErr := decryptPeerSessionKey(existing.SessionKeyCiphertext)
		if keyErr != nil {
			peerError(c, 500, "互联会话密钥不可用")
			return
		}
		registerHTTPPeerSession(existing, key)
		peerAudit(c, "invite_admit_retry", existing.LinkID)
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{
			"link_id": existing.LinkID, "center_id": existing.LocalCenterID,
			"remote_center_id": existing.RemoteCenterID, "remote_group_id": existing.RemoteGroupID,
			"session_id": existing.SessionID, "session_key": interconnect.EncodePeerSessionKey(key),
			"udp_address": centerUDPAddress(c.Request.Host), "send_audio": existing.RemoteSendAudio,
			"receive_audio": existing.RemoteReceiveAudio, "remote_send_audio": existing.LocalSendAudio,
			"remote_receive_audio": existing.LocalReceiveAudio, "virtual_device_name": invite.VirtualDeviceName,
		}})
		return
	}
	// req.LocalGroupID belongs to B. A must not query or create B's local
	// group; B validates that ID in its own database before this request.
	cfg := config.TryGet()
	if cfg == nil {
		peerError(c, 500, "中心配置不可用")
		return
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		peerError(c, 500, "生成会话密钥失败")
		return
	}
	sessionID, err := randomURLToken(24)
	if err != nil {
		peerError(c, 500, "生成会话 ID 失败")
		return
	}
	linkID := invite.InviteID
	if invite.LinkID != "" {
		linkID = invite.LinkID
	}
	encToken, encErr := appcrypto.Encrypt(req.InviteToken)
	if encErr != nil {
		peerError(c, 500, "保存邀请凭据失败")
		return
	}
	encodedKey := interconnect.EncodePeerSessionKey(key)
	encKey, encErr := appcrypto.Encrypt(encodedKey)
	if encErr != nil {
		peerError(c, 500, "保存会话密钥失败")
		return
	}
	link := gormdb.InterCenterLink{LinkID: linkID, LocalCenterID: invite.CenterID, RemoteCenterID: req.CenterID, RemoteAddress: "", LocalGroupID: invite.LocalGroupID, RemoteGroupID: req.LocalGroupID, InitiatorCenterID: invite.CenterID, Direction: interconnect.CenterLinkDirectionBidirectional, Enabled: true, Accepted: true, ForwardAudio: invite.SendAudio && req.ReceiveAudio || invite.ReceiveAudio && req.SendAudio, VirtualDeviceName: "Server-" + req.CenterID, CredentialHash: invite.TokenHash, CredentialCiphertext: encToken, LocalSendAudio: invite.SendAudio, LocalReceiveAudio: invite.ReceiveAudio, RemoteSendAudio: req.SendAudio, RemoteReceiveAudio: req.ReceiveAudio, SessionID: sessionID, SessionKeyCiphertext: encKey, InviteTokenHash: invite.TokenHash, InviteBound: true, CredentialEpoch: 1}
	if err := centerbridge.Policy(link).Validate(); err != nil {
		peerError(c, 400, "对端中心身份无效")
		return
	}
	existing, getErr := linkRepo.GetByLinkID(linkID)
	if getErr == nil && existing != nil {
		peerError(c, 409, "邀请对应的互联对象已存在，请重新生成邀请")
		return
	}
	if !errors.Is(getErr, gormdb.ErrInterCenterLinkNotFound) {
		peerError(c, 500, "查询互联失败")
		return
	}
	if err := linkRepo.Create(&link); err != nil {
		peerError(c, 500, "保存互联失败")
		return
	}
	if err := repo.Update(invite.ID, map[string]interface{}{"bound": true, "bound_center_id": req.CenterID, "bound_group_id": req.LocalGroupID, "link_id": linkID}); err != nil {
		_ = linkRepo.Delete(link.ID)
		peerError(c, 500, "绑定邀请失败")
		return
	}
	registerHTTPPeerSession(&link, key)
	peerAudit(c, "invite_admit", linkID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"link_id": linkID, "center_id": invite.CenterID, "remote_center_id": req.CenterID, "remote_group_id": invite.LocalGroupID, "session_id": sessionID, "session_key": encodedKey, "udp_address": centerUDPAddress(c.Request.Host), "send_audio": req.SendAudio, "receive_audio": req.ReceiveAudio, "remote_send_audio": invite.SendAudio, "remote_receive_audio": invite.ReceiveAudio, "virtual_device_name": invite.VirtualDeviceName}})
}

func AdmitCenterPeerInvite(c *gin.Context) { admitPeerInvite(c) }

func decryptPeerSessionKey(ciphertext string) ([]byte, error) {
	encoded, err := appcrypto.Decrypt(ciphertext)
	if err != nil {
		return nil, err
	}
	key, err := interconnect.DecodePeerSessionKey(encoded)
	if err != nil || len(key) != 32 {
		if err == nil {
			err = errors.New("invalid peer session key length")
		}
		return nil, err
	}
	return key, nil
}

func registerHTTPPeerSession(link *gormdb.InterCenterLink, key []byte) {
	if link == nil {
		return
	}
	runtime := interconnect.ActiveCenterRuntime()
	if runtime == nil || runtime.HTTPPeers == nil {
		return
	}
	err := runtime.HTTPPeers.RegisterSession(interconnect.CenterHTTPPeerSession{
		Link: interconnect.CenterPeerLink{
			LinkID: link.LinkID, LocalCenterID: link.LocalCenterID, RemoteCenterID: link.RemoteCenterID,
			LocalGroupID: link.LocalGroupID, RemoteGroupID: link.RemoteGroupID, InitiatorCenterID: link.InitiatorCenterID,
			Direction: link.Direction, Enabled: link.Enabled, Accepted: link.Accepted, ForwardAudio: link.ForwardAudio,
			VirtualDeviceName: link.VirtualDeviceName, CredentialEpoch: link.CredentialEpoch,
		},
		SessionID: link.SessionID, SessionKey: key, RemoteUDP: link.RemoteUDPAddress,
		LocalSendAudio: link.LocalSendAudio, LocalReceiveAudio: link.LocalReceiveAudio,
		RemoteSendAudio: link.RemoteSendAudio, RemoteReceiveAudio: link.RemoteReceiveAudio,
	})
	if err != nil {
		log.Printf("[CENTER-PEER] restore admitted session %s failed: %v", link.LinkID, err)
	}
}

func ImportCenterPeerInvite(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var req importPeerInviteRequest
	if c.ShouldBindJSON(&req) != nil {
		peerError(c, 400, "导入参数错误")
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.RemoteAddress))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		peerError(c, 400, "远端地址必须是 http 或 https URL")
		return
	}
	if len(req.InviteToken) < 40 {
		peerError(c, 400, "Token 太短")
		return
	}
	cfg := config.TryGet()
	if cfg == nil || cfg.Interconnect.CenterID == "" {
		peerError(c, 500, "未配置本地中心 ID")
		return
	}
	group, err := gormdb.NewGroupRepository().GetGroupByID(req.LocalGroupID)
	if err != nil || !canUseGroupAsLinkTarget(group) {
		peerError(c, 400, "本地群组必须是已启用的实体群组")
		return
	}
	body := admitPeerInviteRequest{InviteToken: req.InviteToken, CenterID: cfg.Interconnect.CenterID, LocalGroupID: req.LocalGroupID, SendAudio: req.SendAudio, ReceiveAudio: req.ReceiveAudio}
	data, err := jsonMarshal(body)
	if err != nil {
		peerError(c, 500, "生成准入请求失败")
		return
	}
	httpClient := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	admitURL := strings.TrimRight(u.String(), "/") + "/api/inter-center/admit"
	request, err := http.NewRequest(http.MethodPost, admitURL, strings.NewReader(string(data)))
	if err != nil {
		peerError(c, 400, "远端地址无效")
		return
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		peerError(c, 502, "连接远端中心失败")
		return
	}
	defer response.Body.Close()
	var envelope struct {
		Data struct {
			LinkID             string `json:"link_id"`
			CenterID           string `json:"center_id"`
			RemoteCenterID     string `json:"remote_center_id"`
			SessionID          string `json:"session_id"`
			SessionKey         string `json:"session_key"`
			UDPAddress         string `json:"udp_address"`
			VirtualDeviceName  string `json:"virtual_device_name"`
			RemoteGroupID      int    `json:"remote_group_id"`
			SendAudio          bool   `json:"send_audio"`
			ReceiveAudio       bool   `json:"receive_audio"`
			RemoteSendAudio    bool   `json:"remote_send_audio"`
			RemoteReceiveAudio bool   `json:"remote_receive_audio"`
		} `json:"data"`
	}
	if err := decodeJSON(response.Body, &envelope); err != nil {
		log.Printf("[CENTER-PEER] invalid admission response from %s: %v", admitURL, err)
		peerError(c, 502, "远端响应格式无效")
		return
	}
	if response.StatusCode != http.StatusOK || envelope.Data.SessionKey == "" {
		log.Printf("[CENTER-PEER] admission rejected status=%d session_key_len=%d", response.StatusCode, len(envelope.Data.SessionKey))
		if response.StatusCode != http.StatusOK {
			peerError(c, 502, "远端拒绝互联邀请（HTTP 状态异常）")
		} else {
			peerError(c, 502, "远端响应缺少会话密钥")
		}
		return
	}
	key, err := interconnect.DecodePeerSessionKey(envelope.Data.SessionKey)
	if err != nil || len(key) != 32 || envelope.Data.LinkID == "" || envelope.Data.CenterID == "" || envelope.Data.CenterID == cfg.Interconnect.CenterID || len(envelope.Data.SessionID) != 32 || envelope.Data.RemoteGroupID <= 0 || envelope.Data.UDPAddress == "" {
		peerError(c, 502, "远端会话密钥无效")
		return
	}
	if _, err := net.ResolveUDPAddr("udp", envelope.Data.UDPAddress); err != nil {
		peerError(c, 502, "远端 UDP 地址无效")
		return
	}
	encKey, err := appcrypto.Encrypt(envelope.Data.SessionKey)
	if err != nil {
		peerError(c, 500, "保存会话密钥失败")
		return
	}
	hash := gormdb.HashCenterPeerInviteToken(req.InviteToken)
	virtualName := envelope.Data.VirtualDeviceName
	if virtualName == "" {
		virtualName = "Server-" + envelope.Data.CenterID
	}
	link := &gormdb.InterCenterLink{LinkID: envelope.Data.LinkID, LocalCenterID: cfg.Interconnect.CenterID, RemoteCenterID: envelope.Data.CenterID, RemoteAddress: req.RemoteAddress, LocalGroupID: req.LocalGroupID, RemoteGroupID: envelope.Data.RemoteGroupID, InitiatorCenterID: envelope.Data.CenterID, Direction: interconnect.CenterLinkDirectionBidirectional, Enabled: true, Accepted: true, ForwardAudio: req.SendAudio && envelope.Data.RemoteReceiveAudio || req.ReceiveAudio && envelope.Data.RemoteSendAudio, VirtualDeviceName: virtualName, CredentialHash: hash, CredentialCiphertext: encKey, LocalSendAudio: req.SendAudio, LocalReceiveAudio: req.ReceiveAudio, RemoteSendAudio: envelope.Data.RemoteSendAudio, RemoteReceiveAudio: envelope.Data.RemoteReceiveAudio, SessionID: envelope.Data.SessionID, SessionKeyCiphertext: encKey, RemoteUDPAddress: envelope.Data.UDPAddress, InviteTokenHash: hash, InviteBound: true, CredentialEpoch: 1}
	if err := centerbridge.Policy(*link).Validate(); err != nil {
		peerError(c, 502, "远端互联身份无效")
		return
	}
	linkRepo := gormdb.NewInterCenterLinkRepository()
	existing, getErr := linkRepo.GetByLinkID(link.LinkID)
	if getErr == nil && existing != nil {
		if existing.LocalCenterID != link.LocalCenterID || existing.RemoteCenterID != link.RemoteCenterID || existing.LocalGroupID != link.LocalGroupID {
			peerError(c, 409, "互联对象与当前中心配置冲突")
			return
		}
		link.ID = existing.ID
		if err := linkRepo.Update(existing.ID, map[string]interface{}{
			"remote_address": link.RemoteAddress, "remote_group_id": link.RemoteGroupID,
			"forward_audio": link.ForwardAudio, "local_send_audio": link.LocalSendAudio,
			"local_receive_audio": link.LocalReceiveAudio, "remote_send_audio": link.RemoteSendAudio,
			"remote_receive_audio": link.RemoteReceiveAudio, "session_id": link.SessionID,
			"session_key_ciphertext": link.SessionKeyCiphertext, "remote_udp_address": link.RemoteUDPAddress,
			"invite_token_hash": link.InviteTokenHash, "invite_bound": true, "accepted": true, "enabled": true,
		}); err != nil {
			peerError(c, 500, "更新本地互联失败")
			return
		}
	} else if errors.Is(getErr, gormdb.ErrInterCenterLinkNotFound) {
		if err := linkRepo.Create(link); err != nil {
			peerError(c, 500, "保存本地互联失败")
			return
		}
	} else {
		peerError(c, 500, "查询本地互联失败")
		return
	}
	registerHTTPPeerSession(link, key)
	if runtime := interconnect.ActiveCenterRuntime(); runtime != nil && runtime.HTTPPeers != nil {
		_ = runtime.HTTPPeers.Bind(link.LinkID)
	}
	peerAudit(c, "invite_import", link.LinkID)
	c.JSON(http.StatusOK, gin.H{"code": 200, "message": "互联已建立", "data": gin.H{"link_id": link.LinkID}})
}

// Small wrappers keep the admission code independent from the project's
// JSON helper implementation and make bounded body decoding explicit.
func jsonMarshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(io.LimitReader(r, 64<<10)).Decode(v)
}
