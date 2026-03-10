package udphub

import (
	"net"
	"time"

	"nrllink/internal/models"
	"nrllink/internal/protocol"
)

// forwardVoice 转发语音消息
func forwardVoice(nrl *models.NRL2Packet, dev *models.Device, packet []byte, gp *models.Group) {
	numbs := len(gp.ConnPool.(*CurrentConnPool).DevConnMap)

	// 房间类型为中继互联时，不允许出现双工
	if gp.Type == models.GroupTypeRelay || gp.ID == models.GroupIDPublicMin {
		numbs = 3
	}

	switch numbs {
	case 0:
		return

	case 1:
		// 只有一个设备，环路测试
		if globalConn != nil {
			globalConn.WriteToUDP(packet, nrl.UDPAddr)
		}
		gp.ConnPool.(*CurrentConnPool).UDPAddr = nrl.UDPAddr
		gp.ConnPool.(*CurrentConnPool).LastVoiceTime = nrl.TimeStamp

	case 2:
		// 两个设备，全双工通信
		for _, vv := range gp.ConnPool.(*CurrentConnPool).DevConnMap {
			// 转发给其他设备，不包含自己
			if vv.UDPAddr != nil && nrl.UDPAddrStr != vv.UDPAddr.String() && (vv.Status&models.DevStatusRxDisable) != models.DevStatusRxDisable {
				// 普通设备转发给 200 设备
				if vv.DevModel == models.DevModelServer && (vv.Status&models.DevStatusNoRelay) != models.DevStatusNoRelay {
					newPacket := protocol.Replace200and255Dev(vv.CallSign, vv.SSID, nrl.Type, models.DevModelServer,
						nrl.CallSign, nrl.SSID, nrl.UDPAddr.IP.To4(), dev.DMRID, packet)
					if globalConn != nil {
						globalConn.WriteToUDP(newPacket, vv.UDPAddr)
					}
				} else {
					if globalConn != nil {
						globalConn.WriteToUDP(packet, vv.UDPAddr)
					}
				}
			} else {
				// 更新自己的时间
				vv.LastVoiceEndTime = nrl.TimeStamp
			}
		}

	default:
		// 3 个或以上设备，只允许一个设备发送语音

		// 会议模式需要混音
		if gp.Type == models.GroupTypeMeeting && nrl.Type == models.TypeG711Voice && len(nrl.DATA) == 160 {
			voiceData := make([]byte, len(nrl.DATA))
			copy(voiceData, nrl.DATA)
			select {
			case dev.PcmG711Chan <- [][]byte{voiceData}:
			default:
			}
			return
		}

		// 优先级控制
		if dev.Priority <= gp.ConnPool.(*CurrentConnPool).LastPriority &&
			nrl.UDPAddrStr != gp.ConnPool.(*CurrentConnPool).UDPAddr.String() &&
			nrl.TimeStamp.Sub(gp.ConnPool.(*CurrentConnPool).LastVoiceTime) < 200*time.Millisecond {

			dev.LastVoiceEndTime = nrl.TimeStamp
			return
		} else {
			gp.ConnPool.(*CurrentConnPool).UDPAddr = nrl.UDPAddr
			gp.ConnPool.(*CurrentConnPool).LastVoiceTime = nrl.TimeStamp
			gp.ConnPool.(*CurrentConnPool).LastPriority = dev.Priority
		}

		for _, vv := range gp.ConnPool.(*CurrentConnPool).DevConnList {
			if vv.UDPAddr != nil && nrl.UDPAddrStr != vv.UDPAddr.String() && (vv.Status&models.DevStatusRxDisable) != models.DevStatusRxDisable {
				if vv.DevModel == models.DevModelServer {
					newPacket := protocol.Replace200and255Dev(vv.CallSign, vv.SSID, nrl.Type, models.DevModelServer,
						nrl.CallSign, nrl.SSID, nrl.UDPAddr.IP.To4(), dev.DMRID, packet)
					if globalConn != nil {
						globalConn.WriteToUDP(newPacket, vv.UDPAddr)
					}
				} else if vv.DevModel == models.DevModelFullNet || vv.SSID == models.SSIDServerMax {
					continue
				} else {
					if globalConn != nil {
						globalConn.WriteToUDP(packet, vv.UDPAddr)
					}
				}
			} else {
				vv.LastVoiceEndTime = nrl.TimeStamp
			}
		}

		// 999 房间需要输出到全网通
		if gp.ID == models.GroupIDPublicMin {
			fullNetOutput(nrl, dev, packet)
		}
	}
}

// forwardServerVoice 转发服务器语音
func forwardServerVoice(nrl *models.NRL2Packet, dev *models.Device, packet []byte, conn *net.UDPConn, gp *models.Group) {
	originalCallsignSSID := protocol.GetCallSignSSID(nrl.OriginalCallsign, nrl.OriginalSSID)

	if q, ok := qthMapNew[originalCallsignSSID]; !ok {
		update200QTH(originalCallsignSSID, nrl, dev)
	} else if time.Since(q.JoinTime) > 10*time.Minute {
		update200QTH(originalCallsignSSID, nrl, dev)
	}

	if (nrl.UDPAddrStr != gp.ConnPool.(*CurrentConnPool).UDPAddr.String()) && nrl.TimeStamp.Sub(gp.ConnPool.(*CurrentConnPool).LastVoiceTime) < 200*time.Millisecond {
		if k, ok := gp.ConnPool.(*CurrentConnPool).DevConnMap[nrl.UDPAddrStr]; ok {
			k.LastVoiceEndTime = nrl.TimeStamp
		}
		return
	} else {
		gp.ConnPool.(*CurrentConnPool).UDPAddr = nrl.UDPAddr
		gp.ConnPool.(*CurrentConnPool).LastVoiceTime = nrl.TimeStamp
	}

	var newPacket []byte

	if nrl.DevModel == models.DevModelServer {
		newPacket = protocol.Replace200and255Dev(nrl.OriginalCallsign, nrl.OriginalSSID, nrl.Type, models.DevModelServer,
			nrl.CallSign, nrl.SSID, nrl.OriginalIP, nrl.DMRID, packet)
	} else if nrl.DevModel == models.DevModelFullNet && nrl.SSID == models.SSIDServerMax {
		newPacket = protocol.Replace200and255Dev(nrl.OriginalCallsign, nrl.OriginalSSID, nrl.Type, models.DevModelServer,
			nrl.CallSign, nrl.SSID, nrl.OriginalIP, nrl.DMRID, packet)
	}

	for _, vv := range gp.ConnPool.(*CurrentConnPool).DevConnList {
		if vv.UDPAddr != nil && nrl.UDPAddrStr != vv.UDPAddr.String() && (vv.Status&models.DevStatusRxDisable) != models.DevStatusRxDisable {
			// 转发给 200 设备
			if vv.DevModel == models.DevModelServer {
				new200Packet := protocol.Replace200and255Dev(vv.CallSign, vv.SSID, nrl.Type, models.DevModelServer,
					nrl.OriginalCallsign, nrl.OriginalSSID, nrl.OriginalIP, nrl.DMRID, packet)
				conn.WriteToUDP(new200Packet, vv.UDPAddr)
			} else if nrl.DevModel == models.DevModelFullNet && nrl.SSID == models.SSIDServerMax && vv.DevModel != models.DevModelFullNet && vv.SSID != models.SSIDServerMax {
				conn.WriteToUDP(newPacket, vv.UDPAddr)
			} else {
				conn.WriteToUDP(newPacket, vv.UDPAddr)
			}
		} else {
			vv.LastVoiceEndTime = nrl.TimeStamp
		}
	}
}

// forwardMessage 转发文本消息
func forwardMessage(nrl *models.NRL2Packet, packet []byte, dev *models.Device, conn *net.UDPConn, connPool *CurrentConnPool) {
	clientAddrStr := nrl.UDPAddr.String()

	// 200 设备转发
	if nrl.DevModel == models.DevModelServer {
		if dev.GroupID == models.GroupIDPublicMin {
			return
		}

		newPacket := protocol.Replace200and255Dev(nrl.OriginalCallsign, nrl.OriginalSSID, nrl.Type, models.DevModelServer,
			nrl.CallSign, nrl.SSID, nrl.OriginalIP, nrl.DMRID, packet)

		for kk, vv := range connPool.DevConnMap {
			if clientAddrStr != kk {
				if vv.DevModel == models.DevModelServer {
					newPacket := protocol.Replace200and255Dev(vv.CallSign, vv.SSID, nrl.Type, models.DevModelServer,
						nrl.OriginalCallsign, nrl.OriginalSSID, nrl.OriginalIP, nrl.DMRID, packet)
					conn.WriteToUDP(newPacket, vv.UDPAddr)
				} else if vv.DevModel == models.DevModelFullNet || vv.SSID == models.SSIDServerMax {
					continue
				} else {
					conn.WriteToUDP(newPacket, vv.UDPAddr)
				}
			}
		}
		return
	}

	// 255 设备转发
	if nrl.DevModel == models.DevModelFullNet || nrl.SSID == models.SSIDServerMax {
		newPacket := protocol.Replace200and255Dev(nrl.OriginalCallsign, nrl.OriginalSSID, nrl.Type, models.SSIDServerMax,
			nrl.CallSign, nrl.SSID, nrl.OriginalIP, nrl.DMRID, packet)

		for kk, vv := range connPool.DevConnMap {
			if clientAddrStr != kk {
				if vv.DevModel == models.DevModelFullNet || vv.SSID == models.SSIDServerMax {
					continue
				} else if vv.DevModel == models.DevModelServer {
					continue
				} else {
					conn.WriteToUDP(newPacket, vv.UDPAddr)
				}
			}
		}
		return
	}

	// 普通设备转发
	for kk, vv := range connPool.DevConnMap {
		if clientAddrStr != kk {
			if vv.DevModel == models.DevModelServer {
				if dev.GroupID == models.GroupIDPublicMin {
					continue
				}
				newPacket := protocol.Replace200and255Dev(vv.CallSign, vv.SSID, nrl.Type, models.DevModelServer,
					nrl.CallSign, nrl.SSID, nrl.UDPAddr.IP.To4(), vv.DMRID, packet)
				conn.WriteToUDP(newPacket, vv.UDPAddr)
			} else if vv.DevModel == models.DevModelFullNet || vv.SSID == models.SSIDServerMax {
				continue
			} else {
				conn.WriteToUDP(packet, vv.UDPAddr)
			}
		}
	}

	// 普通设备转发到全网通
	if dev.GroupID == models.GroupIDPublicMin {
		fullNetOutput(nrl, dev, packet)
	}
}

// forwardControl 转发控制信号
func forwardControl(nrl *models.NRL2Packet, packet []byte, conn *net.UDPConn, gp *models.Group) {
	numbs := len(gp.ConnPool.(*CurrentConnPool).DevConnMap)

	// 房间类型为中继互联时，不允许出现双工
	if gp.Type == models.GroupTypeRelay {
		numbs = 3
	}

	switch numbs {
	case 0:
		return

	case 1:
		conn.WriteToUDP(packet, nrl.UDPAddr)
		gp.ConnPool.(*CurrentConnPool).UDPAddr = nrl.UDPAddr
		gp.ConnPool.(*CurrentConnPool).LastCtlTime = nrl.TimeStamp

	case 2:
		for kk, vv := range gp.ConnPool.(*CurrentConnPool).DevConnMap {
			if nrl.UDPAddrStr != kk && (vv.Status&models.DevStatusRxDisable) != models.DevStatusRxDisable {
				if vv.DevModel == models.DevModelServer {
					continue
				} else {
					conn.WriteToUDP(packet, vv.UDPAddr)
				}
			} else {
				vv.LastCtlEndTime = nrl.TimeStamp
			}
		}

	default:
		if nrl.UDPAddrStr != gp.ConnPool.(*CurrentConnPool).UDPAddr.String() && nrl.TimeStamp.Sub(gp.ConnPool.(*CurrentConnPool).LastCtlTime) < 200*time.Millisecond {
			if k, ok := gp.ConnPool.(*CurrentConnPool).DevConnMap[nrl.UDPAddrStr]; ok {
				k.LastCtlEndTime = nrl.TimeStamp
			}
			return
		} else {
			gp.ConnPool.(*CurrentConnPool).UDPAddr = nrl.UDPAddr
			gp.ConnPool.(*CurrentConnPool).LastCtlTime = nrl.TimeStamp
		}

		for kk, vv := range gp.ConnPool.(*CurrentConnPool).DevConnMap {
			if vv.UDPAddr != nil && nrl.UDPAddrStr != kk && (vv.Status&models.DevStatusRxDisable) != models.DevStatusRxDisable {
				if vv.DevModel == models.DevModelServer {
					continue
				} else {
					conn.WriteToUDP(packet, vv.UDPAddr)
				}
			} else {
				vv.LastCtlEndTime = nrl.TimeStamp
			}
		}
	}
}

// fullNetOutput 全网通输出
func fullNetOutput(nrl *models.NRL2Packet, dev *models.Device, packet []byte) {
	// TODO: 实现全网通输出逻辑
	// 这个功能需要向其他平台转发数据
}

// update200QTH 更新 200 设备 QTH
func update200QTH(originalCallsignSSID string, nrl *models.NRL2Packet, dev *models.Device) {
	callsignSSID := protocol.GetCallSignSSID(nrl.CallSign, nrl.SSID)
	originalQTH := getQTH(nrl.OriginalIP.String())

	qthMap[originalCallsignSSID] = callsignSSID + "-" + originalQTH

	qthMapNew[originalCallsignSSID] = models.QTH{
		QTH:          originalQTH,
		CallSignSSID: originalCallsignSSID,
		JoinTime:     time.Now(),
		Name:         callsignSSID + "-" + dev.Name,
	}
}

// GetGlobalConn 获取全局 UDP 连接
func GetGlobalConn() *net.UDPConn {
	return globalConn
}
