package udphub

import (
	"log"
	"strconv"
	"strings"
	"time"

	"nrllink/internal/gormdb"
	"nrllink/internal/models"
	"nrllink/internal/protocol"
)

// initPublicGroups 初始化公共群组
func initPublicGroups() {
	// 创建默认群组 0
	publicGroupMap[0] = &models.Group{
		ID:           0,
		Name:         "公共大厅",
		OwerCallSign: "default",
		Status:       1,
		DevMap:       make(map[int]*models.Device),
		CreateTime:   time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime:   time.Now().Format("2006-01-02 15:04:05"),
		ConnPool:     &CurrentConnPool{DevConnMap: make(map[string]*models.Device)},
	}

	// 创建全网通群组 999
	publicGroupMap[models.GroupIDPublicMin] = &models.Group{
		ID:           models.GroupIDPublicMin,
		Name:         "全网互联",
		OwerCallSign: "default",
		Status:       1,
		DevMap:       make(map[int]*models.Device),
		CreateTime:   time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime:   time.Now().Format("2006-01-02 15:04:05"),
		ConnPool:     &CurrentConnPool{DevConnMap: make(map[string]*models.Device)},
	}

	// 从数据库加载公共群组
	repo := gormdb.NewGroupRepository()
	groups, err := repo.ListPublicGroups()
	if err != nil {
		log.Printf("Load public groups from database failed: %v", err)
		return
	}

	for _, gp := range groups {
		newGroup := gp.ToModelGroup()
		newGroup.ConnPool = &CurrentConnPool{DevConnMap: make(map[string]*models.Device)}
		newGroup.DevMap = make(map[int]*models.Device)

		publicGroupMap[newGroup.ID] = newGroup

		// 会议模式启动混音
		if newGroup.Type == models.GroupTypeMeeting {
			go startMixPCM(newGroup)
		}

		log.Printf("Loaded public group: %d - %s (type: %d)", newGroup.ID, newGroup.Name, newGroup.Type)
	}

	log.Printf("Initialized %d public groups", len(publicGroupMap))
}

// GetPublicGroup 获取公共群组
func GetPublicGroup(id int) (*models.Group, bool) {
	gp, ok := publicGroupMap[id]
	return gp, ok
}

// GetAllPublicGroups 获取所有公共群组
func GetAllPublicGroups() map[int]*models.Group {
	return publicGroupMap
}

// CreatePublicGroup 创建公共群组
func CreatePublicGroup(gp *models.Group) error {
	repo := gormdb.NewGroupRepository()

	gormGroup := gormdb.FromModelGroup(gp)
	gormGroup.Status = 1

	if err := repo.CreateGroup(gormGroup); err != nil {
		return err
	}

	// 更新 models.Group 的 ID
	gp.ID = gormGroup.ID

	newGroup := &models.Group{
		ID:                gp.ID,
		Name:              gp.Name,
		Type:              gp.Type,
		CallSign:          gp.CallSign,
		Password:          gp.Password,
		AllowCallSignSSID: gp.AllowCallSignSSID,
		OwerID:            gp.OwerID,
		OwerCallSign:      gp.OwerCallSign,
		DevList:           gp.DevList,
		MasterServer:      gp.MasterServer,
		SlaveServer:       gp.SlaveServer,
		Status:            1,
		CreateTime:        time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime:        time.Now().Format("2006-01-02 15:04:05"),
		Note:              gp.Note,
		ConnPool:          &CurrentConnPool{DevConnMap: make(map[string]*models.Device)},
		DevMap:            make(map[int]*models.Device),
	}

	publicGroupMap[newGroup.ID] = newGroup

	// 会议模式启动混音
	if newGroup.Type == models.GroupTypeMeeting {
		go startMixPCM(newGroup)
	}

	return nil
}

// UpdatePublicGroup 更新公共群组
func UpdatePublicGroup(gp *models.Group) error {
	repo := gormdb.NewGroupRepository()

	gormGroup := gormdb.FromModelGroup(gp)
	if err := repo.UpdateGroup(gormGroup); err != nil {
		return err
	}

	if existing, ok := publicGroupMap[gp.ID]; ok {
		existing.Name = gp.Name
		existing.Type = gp.Type
		existing.CallSign = gp.CallSign
		existing.Password = gp.Password
		existing.AllowCallSignSSID = gp.AllowCallSignSSID
		existing.Note = gp.Note
		existing.UpdateTime = time.Now().Format("2006-01-02 15:04:05")
	}

	return nil
}

// DeletePublicGroup 删除公共群组
func DeletePublicGroup(id int) error {
	repo := gormdb.NewGroupRepository()

	if err := repo.DeleteGroup(id); err != nil {
		return err
	}

	delete(publicGroupMap, id)
	return nil
}

// startMixPCM 会议模式混音（独立函数，不是方法）
func startMixPCM(g *models.Group) {
	pool := getGroupConnPool(g)
	if pool == nil {
		g.ConnPool = &CurrentConnPool{DevConnMap: make(map[string]*models.Device)}
		pool = g.ConnPool.(*CurrentConnPool)
	}

	pcm := make([]int, 160)
	globalG711 := make([]byte, 160)
	newG711 := make([]byte, 160)
	data := make([]byte, 160)

	// 使用 DraARLv1 协议编码会议混音包
	globalPacket := protocol.EncodeDraARLv1("MEETLY", "", 201, protocol.DraARLTypeG711Voice, 201, 0, "MEETLY", data)
	speakerPacket := protocol.EncodeDraARLv1("MEETLY", "", 201, protocol.DraARLTypeG711Voice, 201, 0, "MEETLY", data)
	speakerBPacket := protocol.EncodeDraARLv1("MEETLY", "", 201, protocol.DraARLTypeG711Voice, 201, 0, "MEETLY", data)

	log.Printf("Starting mixPCM for group: %d - %s", g.ID, g.Name)

	type activeSpeaker struct {
		dev     *models.Device
		rawG711 []byte
	}
	speakers := make([]activeSpeaker, 0, 10)

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		speakers = speakers[:0]
		for i := range pcm {
			pcm[i] = 0
		}

		if pool.DevConnList == nil || len(pool.DevConnList) <= 2 {
			continue
		}

		// 收集发言者数据
		for _, vv := range pool.DevConnList {
			if vv.Speaking != nil {
				*vv.Speaking = false
			}
			select {
			case g711 := <-vv.PcmG711Chan:
				if len(g711) > 0 {
					raw := g711[0]
					if len(raw) >= 160 {
						raw = raw[:160]
					}
					vv.Speaking = new(bool)
					*vv.Speaking = true
					speakers = append(speakers, activeSpeaker{vv, raw})
				}
			default:
			}
		}

		numbs := len(speakers)
		if numbs == 0 {
			continue
		}

		// 单人发言直通
		if numbs == 1 {
			copy(globalPacket[93:], speakers[0].rawG711)

			for _, vv := range pool.DevConnList {
				if vv.UDPAddr == nil || (vv.Speaking != nil && *vv.Speaking) {
					continue
				}
				if globalConn != nil {
					globalConn.WriteToUDP(globalPacket, vv.UDPAddr)
				}
			}
		} else {
			// 多人混音处理
			for _, s := range speakers {
				for i, v := range s.rawG711 {
					if i < len(s.dev.PcmBuffer) {
						ori := int(alaw2linear(v))
						pcm[i] += ori
						s.dev.PcmBuffer[i] = ori
					}
				}
			}

			// 计算全局混合音
			for i, v := range pcm {
				if v > 32767 {
					v = 32767
				} else if v < -32768 {
					v = -32768
				}
				globalG711[i] = linear2Alaw(int16(v))
			}
			copy(globalPacket[93:], globalG711)

			if numbs == 2 {
				// 双人对讲互传
				if len(speakers[0].rawG711) >= 160 {
					copy(speakerPacket[93:], speakers[0].rawG711[:160])
				}
				if len(speakers[1].rawG711) >= 160 {
					copy(speakerBPacket[93:], speakers[1].rawG711[:160])
				}

				for _, vv := range pool.DevConnList {
					if vv.UDPAddr == nil {
						continue
					}
					if vv.Speaking != nil && *vv.Speaking && len(speakers) > 1 {
						if globalConn != nil {
							globalConn.WriteToUDP(speakerBPacket, vv.UDPAddr)
						}
					} else if vv.Speaking != nil && *vv.Speaking && len(speakers) > 0 {
						if globalConn != nil {
							globalConn.WriteToUDP(speakerPacket, vv.UDPAddr)
						}
					} else {
						if globalConn != nil {
							globalConn.WriteToUDP(globalPacket, vv.UDPAddr)
						}
					}
				}
			} else {
				// 3 人及以上标准混音
				for _, vv := range pool.DevConnList {
					if vv.UDPAddr == nil {
						continue
					}
					if vv.Speaking != nil && *vv.Speaking {
						for i, v := range pcm {
							v -= vv.PcmBuffer[i]
							if v > 32767 {
								v = 32767
							} else if v < -32768 {
								v = -32768
							}
							newG711[i] = linear2Alaw(int16(v))
						}
						copy(speakerPacket[93:], newG711)
						if globalConn != nil {
							globalConn.WriteToUDP(speakerPacket, vv.UDPAddr)
						}
					} else {
						if globalConn != nil {
							globalConn.WriteToUDP(globalPacket, vv.UDPAddr)
						}
					}
				}
			}
		}
	}
}

// G.711 编解码表
var (
	alaw2linearTable = [256]int16{}
	linear2alawTable = [65536]byte{}
)

func init() {
	// 初始化 G.711 编解码表
	for i := range 256 {
		alaw2linearTable[i] = rawAlaw2linear(byte(i))
	}

	for i := range 65536 {
		linear2alawTable[i] = rawLinear2Alaw(int16(i))
	}
}

func rawAlaw2linear(code byte) int16 {
	code ^= 0x55

	iexp := int16((code & 0x70) >> 4)
	mant := int16(code & 0x0F)

	if iexp > 0 {
		mant += 16
	}

	mant = (mant << 4) + 8

	if iexp > 1 {
		mant <<= (iexp - 1)
	}

	if (code & 0x80) != 0 {
		return mant
	}
	return -mant
}

func rawLinear2Alaw(sample int16) byte {
	var sign byte
	var ix int16

	if sample < 0 {
		sign = 0x80
		ix = (^sample) >> 4
	} else {
		ix = sample >> 4
	}

	if ix > 15 {
		iexp := byte(1)
		for ix > 31 {
			ix >>= 1
			iexp++
		}
		ix -= 16
		ix += int16(iexp << 4)
	}

	if sign == 0 {
		ix |= 0x80
	}

	return byte(ix) ^ 0x55
}

func alaw2linear(code byte) int16 {
	return alaw2linearTable[code]
}

func linear2Alaw(sample int16) byte {
	return linear2alawTable[uint16(sample)]
}

// convertStr2IntArray 将字符串转换为整数数组
func convertStr2IntArray(str string) []int {
	s := strings.Split(str, ",")
	res := make([]int, len(s))
	for i, v := range s {
		res[i], _ = strconv.Atoi(v)
	}
	return res
}

// convertIntArray2Str 将整数数组转换为字符串
func convertIntArray2Str(arr []int) string {
	res := make([]string, len(arr))
	for i, v := range arr {
		res[i] = strconv.Itoa(v)
	}
	return strings.Join(res, ",")
}

// GetOnlineDevicesByGroup 获取群组的在线设备
func GetOnlineDevicesByGroup(groupID int) []*models.Device {
	devices := make([]*models.Device, 0)

	if groupID > 0 && groupID <= 3 {
		// 私有群组
		userList.Range(func(k, v any) bool {
			u := v.(*UserInfo)
			if gp, ok := u.Groups[groupID]; ok {
				for _, dev := range gp.DevMap {
					if dev.ISOnline {
						devices = append(devices, dev)
					}
				}
			}
			return true
		})
	} else {
		// 公共群组
		if gp, ok := publicGroupMap[groupID]; ok {
			for _, dev := range gp.DevMap {
				if dev.ISOnline {
					devices = append(devices, dev)
				}
			}
		}
	}

	return devices
}

// GetAllDevicesByGroup 获取群组的所有设备
func GetAllDevicesByGroup(groupID int) []*models.Device {
	devices := make([]*models.Device, 0)

	if groupID > 0 && groupID <= 3 {
		// 私有群组
		userList.Range(func(k, v any) bool {
			u := v.(*UserInfo)
			if gp, ok := u.Groups[groupID]; ok {
				for _, dev := range gp.DevMap {
					devices = append(devices, dev)
				}
			}
			return true
		})
	} else {
		// 公共群组
		if gp, ok := publicGroupMap[groupID]; ok {
			for _, dev := range gp.DevMap {
				devices = append(devices, dev)
			}
		}
	}

	return devices
}

// GroupStats 群组统计信息
type GroupStats struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Type            int    `json:"type"`
	OnlineDevNumber int    `json:"online_dev_number"`
	TotalDevNumber  int    `json:"total_dev_number"`
}

// GetAllGroupStats 获取所有群组统计信息
func GetAllGroupStats() []GroupStats {
	stats := make([]GroupStats, 0)

	// 公共群组
	for _, gp := range publicGroupMap {
		stats = append(stats, GroupStats{
			ID:              gp.ID,
			Name:            gp.Name,
			Type:            gp.Type,
			OnlineDevNumber: gp.OnlineDevNumber,
			TotalDevNumber:  gp.TotalDevNumber,
		})
	}

	// 私有群组
	userList.Range(func(k, v any) bool {
		u := v.(*UserInfo)
		for _, gp := range u.Groups {
			stats = append(stats, GroupStats{
				ID:              gp.ID,
				Name:            gp.Name,
				Type:            gp.Type,
				OnlineDevNumber: gp.OnlineDevNumber,
				TotalDevNumber:  gp.TotalDevNumber,
			})
		}
		return true
	})

	return stats
}
