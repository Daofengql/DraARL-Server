package interconnect

import (
	"errors"
	"fmt"
	"regexp"
)

const (
	CenterLinkDirectionForward       = "one_way_forward"
	CenterLinkDirectionReverse       = "one_way_reverse"
	CenterLinkDirectionBidirectional = "bidirectional"
	CenterLinkMediaAudio             = "audio"
)

var peerIDPattern = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$")
var peerNamePattern = regexp.MustCompile("^Server-[A-Za-z0-9_.-]{1,25}$")

// CenterPeerLink describes an explicitly authorized mapping between independent sites.
type CenterPeerLink struct {
	LinkID            string
	LocalCenterID     string
	RemoteCenterID    string
	LocalGroupID      int
	RemoteGroupID     int
	InitiatorCenterID string
	Direction         string
	Enabled           bool
	Accepted          bool
	ForwardAudio      bool
	ForwardText       bool
	ForwardBroadcast  bool
	VirtualDeviceName string
	CredentialEpoch   uint32
}

func (l CenterPeerLink) Validate() error {
	for _, id := range []string{l.LinkID, l.LocalCenterID, l.RemoteCenterID, l.InitiatorCenterID} {
		if !peerIDPattern.MatchString(id) {
			return errors.New("中心及链路 ID 只能使用 1–64 位字母、数字、点、横线和下划线")
		}
	}
	if l.LocalCenterID == l.RemoteCenterID || l.InitiatorCenterID != l.LocalCenterID && l.InitiatorCenterID != l.RemoteCenterID {
		return errors.New("发起中心必须是两个独立中心之一")
	}
	if l.LocalGroupID <= 0 || l.RemoteGroupID <= 0 || l.LocalGroupID > 2147483647 || l.RemoteGroupID > 2147483647 {
		return errors.New("群组 ID 必须是有效正整数")
	}
	switch l.Direction {
	case CenterLinkDirectionForward, CenterLinkDirectionReverse, CenterLinkDirectionBidirectional:
	default:
		return errors.New("互联方向无效")
	}
	if !peerNamePattern.MatchString(l.VirtualDeviceName) {
		return errors.New("虚拟设备名应为 Server- 前缀加 1–25 位字母、数字、点、横线或下划线")
	}
	if l.ForwardText || l.ForwardBroadcast {
		return errors.New("当前版本仅支持音频互联，文本与自动播报暂未开放")
	}
	return nil
}

func (l CenterPeerLink) AllowsOrigin(origin string) bool {
	if !l.Enabled || !l.Accepted || !l.ForwardAudio || origin != l.LocalCenterID && origin != l.RemoteCenterID {
		return false
	}
	switch l.Direction {
	case CenterLinkDirectionForward:
		return origin == l.InitiatorCenterID
	case CenterLinkDirectionReverse:
		return origin != l.InitiatorCenterID
	case CenterLinkDirectionBidirectional:
		return true
	default:
		return false
	}
}
func (l CenterPeerLink) AllowsMedia(media string) bool {
	return media == CenterLinkMediaAudio && l.ForwardAudio
}

func CenterPeerVirtualDeviceID(remote, link, direction string) string {
	return fmt.Sprintf("intercenter:%s:%s:%s", remote, link, direction)
}
