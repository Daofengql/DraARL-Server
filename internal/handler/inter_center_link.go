package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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

// Inter-center group IDs are local mappings. The update request accepts only
// this center's group, so changing a mapping never changes or exposes the
// peer's local group.
type interCenterLinkUpdateRequest struct {
	LocalGroupID      *int  `json:"local_group_id"`
	Enabled           *bool `json:"enabled"`
	Accepted          *bool `json:"accepted"`
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

// interCenterLinkSummary is the administrator-facing representation. The
// remote group ID is intentionally omitted: it is learned during the HTTP
// admission handshake and is used only by the local routing runtime. Showing
// it here makes it look as if this center can configure the other center's
// group, which violates the per-center ownership boundary.
type interCenterLinkSummary struct {
	ID                int    `json:"id"`
	LinkID            string `json:"link_id"`
	LocalCenterID     string `json:"local_center_id"`
	RemoteCenterID    string `json:"remote_center_id"`
	RemoteAddress     string `json:"remote_address,omitempty"`
	LocalGroupID      int    `json:"local_group_id"`
	InitiatorCenterID string `json:"initiator_center_id"`
	Direction         string `json:"direction"`
	ForwardAudio      bool   `json:"forward_audio"`
	Enabled           bool   `json:"enabled"`
	Accepted          bool   `json:"accepted"`
	CredentialEpoch   uint32 `json:"credential_epoch"`
	VirtualDeviceName string `json:"virtual_device_name"`
	LocalSendAudio    bool   `json:"local_send_audio"`
	LocalReceiveAudio bool   `json:"local_receive_audio"`
}

func summarizeInterCenterLink(l *gormdb.InterCenterLink) interCenterLinkSummary {
	return interCenterLinkSummary{
		ID: l.ID, LinkID: l.LinkID, LocalCenterID: l.LocalCenterID,
		RemoteCenterID: l.RemoteCenterID, RemoteAddress: l.RemoteAddress,
		LocalGroupID: l.LocalGroupID, InitiatorCenterID: l.InitiatorCenterID,
		Direction: l.Direction, ForwardAudio: l.ForwardAudio, Enabled: l.Enabled,
		Accepted: l.Accepted, CredentialEpoch: l.CredentialEpoch,
		VirtualDeviceName: l.VirtualDeviceName, LocalSendAudio: l.LocalSendAudio,
		LocalReceiveAudio: l.LocalReceiveAudio,
	}
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
	return gin.H{"link": summarizeInterCenterLink(l), "runtime": status}
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

// PUT changes only this center's local mapping, state, and media permissions.
// The peer's local mapping is independent and is never accepted here.
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
	var req interCenterLinkUpdateRequest
	if c.ShouldBindJSON(&req) != nil {
		peerError(c, 400, "互联参数错误")
		return
	}
	if req.LocalGroupID == nil && req.Enabled == nil && req.Accepted == nil && req.LocalSendAudio == nil && req.LocalReceiveAudio == nil {
		peerError(c, 400, "没有可更新的本地互联设置")
		return
	}
	l := *old
	if req.LocalGroupID != nil {
		if *req.LocalGroupID <= 0 {
			peerError(c, 400, "本地群组必须是有效正整数")
			return
		}
		group, groupErr := gormdb.NewGroupRepository().GetGroupByID(*req.LocalGroupID)
		if groupErr != nil || !canUseGroupAsLinkTarget(group) {
			peerError(c, 400, "本地群组必须是已启用的实体群组")
			return
		}
		l.LocalGroupID = *req.LocalGroupID
	}
	if req.Enabled != nil {
		l.Enabled = *req.Enabled
	}
	if req.Accepted != nil {
		l.Accepted = *req.Accepted
	}
	if req.LocalSendAudio != nil {
		l.LocalSendAudio = *req.LocalSendAudio
	}
	if req.LocalReceiveAudio != nil {
		l.LocalReceiveAudio = *req.LocalReceiveAudio
	}
	if err := centerbridge.Policy(l).Validate(); err != nil {
		peerError(c, 400, err.Error())
		return
	}
	fields := map[string]interface{}{"local_group_id": l.LocalGroupID, "enabled": l.Enabled, "accepted": l.Accepted, "local_send_audio": l.LocalSendAudio, "local_receive_audio": l.LocalReceiveAudio}
	if err := repo.Update(id, fields); err != nil {
		peerError(c, 500, "更新互联对象失败")
		return
	}
	pc, err := centerbridge.Config(l)
	if err != nil {
		peerError(c, 500, "读取互联凭据失败")
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
	InviteID     string `json:"invite_id" binding:"required"`
	TokenProof   string `json:"token_proof" binding:"required"`
	ClientNonce  string `json:"client_nonce" binding:"required"`
	CenterID     string `json:"center_id" binding:"required"`
	LocalGroupID int    `json:"local_group_id" binding:"required"`
	SendAudio    bool   `json:"send_audio"`
	ReceiveAudio bool   `json:"receive_audio"`
}

var centerPeerInviteIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{15,63}$`)

// Invitation text is an opaque, one-time value for administrators. The
// invite ID is included only so the receiving centre can address the stored
// verifier; the secret portion is never sent to the issuing centre.
func splitCenterPeerInviteToken(value string) (inviteID, secret string, err error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 2 || !centerPeerInviteIDPattern.MatchString(parts[0]) {
		return "", "", errors.New("邀请格式已过期，请重新生成")
	}
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
	if decodeErr != nil || len(decoded) != 32 {
		return "", "", errors.New("邀请格式已过期，请重新生成")
	}
	return parts[0], parts[1], nil
}

func secureEqualString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
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
	secret, err := randomURLToken(32)
	if err != nil {
		peerError(c, 500, "生成邀请失败")
		return
	}
	// Link IDs share the centre identity grammar and therefore must begin with
	// an ASCII letter or digit. Prefixing the 128-bit random value keeps the
	// token URL-safe while eliminating the small invalid-leading-character
	// probability of a raw base64url string.
	inviteID, err := randomURLToken(16)
	if err != nil {
		peerError(c, 500, "生成邀请失败")
		return
	}
	inviteID = "i" + inviteID
	name := strings.TrimSpace(req.VirtualDeviceName)
	if name == "" {
		name = "Server-" + cfg.Interconnect.CenterID
	}
	if !centerPeerNamePattern.MatchString(name) {
		peerError(c, 400, "虚拟设备名称必须以 Server- 开头且不超过 32 个字符")
		return
	}
	// The displayed token carries the public invitation ID and a 256-bit
	// secret. Only the secret's digest is persisted; the secret is later used
	// locally by the importing centre to produce an HTTP admission proof.
	token := inviteID + "." + secret
	hash := gormdb.HashCenterPeerInviteToken(secret)
	ttl := time.Duration(cfg.Interconnect.RegistrationTokenTTL) * time.Second
	invite := &gormdb.CenterPeerInvite{InviteID: inviteID, TokenHash: hash, CenterID: cfg.Interconnect.CenterID, LocalGroupID: req.LocalGroupID, SendAudio: req.SendAudio, ReceiveAudio: req.ReceiveAudio, VirtualDeviceName: name, Enabled: true, ExpiresAt: time.Now().Add(ttl)}
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
	if !centerPeerInviteIDPattern.MatchString(strings.TrimSpace(req.InviteID)) || len(strings.TrimSpace(req.ClientNonce)) < 16 || len(strings.TrimSpace(req.TokenProof)) != 64 {
		peerError(c, 401, "邀请证明无效")
		return
	}
	invite, err := repo.GetByInviteID(req.InviteID)
	if err != nil || invite == nil || !invite.Enabled || !invite.ExpiresAt.IsZero() && !time.Now().Before(invite.ExpiresAt) {
		peerError(c, 401, "邀请 Token 无效或已停用")
		return
	}
	expectedProof, proofErr := interconnect.PeerAdmissionProof(invite.TokenHash, req.InviteID, req.CenterID, req.LocalGroupID, req.SendAudio, req.ReceiveAudio, req.ClientNonce)
	if proofErr != nil || !secureEqualString(expectedProof, strings.TrimSpace(req.TokenProof)) {
		peerError(c, 401, "邀请证明无效")
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
		if getErr != nil || existing == nil || existing.SessionID == "" {
			peerError(c, 409, "邀请绑定状态不完整，请重新生成邀请")
			return
		}
		key, keyErr := interconnect.DerivePeerSessionKey(invite.TokenHash, existing.SessionID)
		if keyErr != nil {
			peerError(c, 500, "互联会话密钥不可用")
			return
		}
		encodedKey := interconnect.EncodePeerSessionKey(key)
		encKey, encErr := appcrypto.Encrypt(encodedKey)
		if encErr != nil || linkRepo.Update(existing.ID, map[string]interface{}{"session_key_ciphertext": encKey}) != nil {
			peerError(c, 500, "更新互联会话密钥失败")
			return
		}
		existing.SessionKeyCiphertext = encKey
		registerHTTPPeerSession(existing, key)
		peerAudit(c, "invite_admit_retry", existing.LinkID)
		udpAddress := centerUDPAddress(c.Request.Host)
		responseProof, proofErr := interconnect.PeerAdmissionResponseProof(
			invite.TokenHash, req.InviteID, existing.LinkID, existing.SessionID,
			existing.LocalCenterID, existing.RemoteCenterID, existing.LocalGroupID, existing.RemoteGroupID,
			existing.RemoteSendAudio, existing.RemoteReceiveAudio, existing.LocalSendAudio, existing.LocalReceiveAudio,
			udpAddress, invite.VirtualDeviceName,
		)
		if proofErr != nil {
			peerError(c, 500, "生成准入响应证明失败")
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{
			"link_id": existing.LinkID, "center_id": existing.LocalCenterID,
			"remote_center_id": existing.RemoteCenterID, "remote_group_id": existing.RemoteGroupID,
			"session_id":      existing.SessionID,
			"admission_proof": responseProof, "udp_address": udpAddress, "send_audio": existing.RemoteSendAudio,
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
	sessionID, err := randomURLToken(24)
	if err != nil {
		peerError(c, 500, "生成会话 ID 失败")
		return
	}
	linkID := invite.InviteID
	if invite.LinkID != "" {
		linkID = invite.LinkID
	}
	key, keyErr := interconnect.DerivePeerSessionKey(invite.TokenHash, sessionID)
	if keyErr != nil {
		peerError(c, 500, "生成会话密钥失败")
		return
	}
	encodedKey := interconnect.EncodePeerSessionKey(key)
	encKey, encErr := appcrypto.Encrypt(encodedKey)
	if encErr != nil {
		peerError(c, 500, "保存会话密钥失败")
		return
	}
	link := gormdb.InterCenterLink{LinkID: linkID, LocalCenterID: invite.CenterID, RemoteCenterID: req.CenterID, RemoteAddress: "", LocalGroupID: invite.LocalGroupID, RemoteGroupID: req.LocalGroupID, InitiatorCenterID: invite.CenterID, Direction: interconnect.CenterLinkDirectionBidirectional, Enabled: true, Accepted: true, ForwardAudio: invite.SendAudio && req.ReceiveAudio || invite.ReceiveAudio && req.SendAudio, VirtualDeviceName: "Server-" + req.CenterID, CredentialHash: invite.TokenHash, CredentialCiphertext: "", LocalSendAudio: invite.SendAudio, LocalReceiveAudio: invite.ReceiveAudio, RemoteSendAudio: req.SendAudio, RemoteReceiveAudio: req.ReceiveAudio, SessionID: sessionID, SessionKeyCiphertext: encKey, InviteTokenHash: invite.TokenHash, InviteBound: true, CredentialEpoch: 1}
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
	udpAddress := centerUDPAddress(c.Request.Host)
	responseProof, proofErr := interconnect.PeerAdmissionResponseProof(
		invite.TokenHash, req.InviteID, linkID, sessionID,
		invite.CenterID, req.CenterID, invite.LocalGroupID, req.LocalGroupID,
		req.SendAudio, req.ReceiveAudio, invite.SendAudio, invite.ReceiveAudio,
		udpAddress, invite.VirtualDeviceName,
	)
	if proofErr != nil {
		peerError(c, 500, "生成准入响应证明失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"link_id": linkID, "center_id": invite.CenterID, "remote_center_id": req.CenterID, "remote_group_id": invite.LocalGroupID, "session_id": sessionID, "admission_proof": responseProof, "udp_address": udpAddress, "send_audio": req.SendAudio, "receive_audio": req.ReceiveAudio, "remote_send_audio": invite.SendAudio, "remote_receive_audio": invite.ReceiveAudio, "virtual_device_name": invite.VirtualDeviceName}})
}

func AdmitCenterPeerInvite(c *gin.Context) { admitPeerInvite(c) }

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
	inviteID, secret, splitErr := splitCenterPeerInviteToken(req.InviteToken)
	if splitErr != nil {
		peerError(c, 400, splitErr.Error())
		return
	}
	tokenHash := gormdb.HashCenterPeerInviteToken(secret)
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
	nonce, err := randomURLToken(16)
	if err != nil {
		peerError(c, 500, "生成准入随机数失败")
		return
	}
	proof, err := interconnect.PeerAdmissionProof(tokenHash, inviteID, cfg.Interconnect.CenterID, req.LocalGroupID, req.SendAudio, req.ReceiveAudio, nonce)
	if err != nil {
		peerError(c, 500, "生成准入证明失败")
		return
	}
	body := admitPeerInviteRequest{InviteID: inviteID, TokenProof: proof, ClientNonce: nonce, CenterID: cfg.Interconnect.CenterID, LocalGroupID: req.LocalGroupID, SendAudio: req.SendAudio, ReceiveAudio: req.ReceiveAudio}
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
			AdmissionProof     string `json:"admission_proof"`
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
	if response.StatusCode != http.StatusOK {
		log.Printf("[CENTER-PEER] admission rejected status=%d", response.StatusCode)
		peerError(c, 502, "远端拒绝互联邀请（HTTP 状态异常）")
		return
	}
	if envelope.Data.LinkID == "" || envelope.Data.CenterID == "" || envelope.Data.CenterID == cfg.Interconnect.CenterID || envelope.Data.RemoteCenterID != cfg.Interconnect.CenterID || len(envelope.Data.SessionID) != 32 || len(envelope.Data.AdmissionProof) != 64 || envelope.Data.RemoteGroupID <= 0 || envelope.Data.UDPAddress == "" {
		peerError(c, 502, "远端会话密钥无效")
		return
	}
	expectedResponseProof, proofErr := interconnect.PeerAdmissionResponseProof(
		tokenHash, inviteID, envelope.Data.LinkID, envelope.Data.SessionID,
		envelope.Data.CenterID, envelope.Data.RemoteCenterID, envelope.Data.RemoteGroupID, req.LocalGroupID,
		envelope.Data.SendAudio, envelope.Data.ReceiveAudio, envelope.Data.RemoteSendAudio, envelope.Data.RemoteReceiveAudio,
		envelope.Data.UDPAddress, envelope.Data.VirtualDeviceName,
	)
	if proofErr != nil || !secureEqualString(expectedResponseProof, strings.TrimSpace(envelope.Data.AdmissionProof)) {
		peerError(c, 502, "远端准入响应证明无效")
		return
	}
	key, err := interconnect.DerivePeerSessionKey(tokenHash, envelope.Data.SessionID)
	if err != nil {
		peerError(c, 502, "远端会话密钥无效")
		return
	}
	if _, err := net.ResolveUDPAddr("udp", envelope.Data.UDPAddress); err != nil {
		peerError(c, 502, "远端 UDP 地址无效")
		return
	}
	encKey, err := appcrypto.Encrypt(interconnect.EncodePeerSessionKey(key))
	if err != nil {
		peerError(c, 500, "保存会话密钥失败")
		return
	}
	hash := tokenHash
	virtualName := envelope.Data.VirtualDeviceName
	if virtualName == "" {
		virtualName = "Server-" + envelope.Data.CenterID
	}
	link := &gormdb.InterCenterLink{LinkID: envelope.Data.LinkID, LocalCenterID: cfg.Interconnect.CenterID, RemoteCenterID: envelope.Data.CenterID, RemoteAddress: req.RemoteAddress, LocalGroupID: req.LocalGroupID, RemoteGroupID: envelope.Data.RemoteGroupID, InitiatorCenterID: envelope.Data.CenterID, Direction: interconnect.CenterLinkDirectionBidirectional, Enabled: true, Accepted: true, ForwardAudio: req.SendAudio && envelope.Data.RemoteReceiveAudio || req.ReceiveAudio && envelope.Data.RemoteSendAudio, VirtualDeviceName: virtualName, CredentialHash: hash, CredentialCiphertext: "", LocalSendAudio: req.SendAudio, LocalReceiveAudio: req.ReceiveAudio, RemoteSendAudio: envelope.Data.RemoteSendAudio, RemoteReceiveAudio: envelope.Data.RemoteReceiveAudio, SessionID: envelope.Data.SessionID, SessionKeyCiphertext: encKey, RemoteUDPAddress: envelope.Data.UDPAddress, InviteTokenHash: hash, InviteBound: true, CredentialEpoch: 1}
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
