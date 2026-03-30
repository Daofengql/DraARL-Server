package udphub

import (
	"log"
	"strconv"
	"strings"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
)

// initPublicGroups 初始化公共群组
func initPublicGroups() {
	// 创建默认群组 0
	publicGroupMap[0] = &models.Group{
		ID:         0,
		Name:       "公共大厅",
		Status:     1,
		DevMap:     make(map[int]*models.Device),
		CreateTime: time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		ConnPool:   &CurrentConnPool{DevConnMap: make(map[string]*models.Device)},
	}

	// 创建全网通群组 999
	publicGroupMap[models.GroupIDPublicMin] = &models.Group{
		ID:         models.GroupIDPublicMin,
		Name:       "全网互联",
		Status:     1,
		DevMap:     make(map[int]*models.Device),
		CreateTime: time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		ConnPool:   &CurrentConnPool{DevConnMap: make(map[string]*models.Device)},
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

	if err := repo.DeleteGroupWithCascade(id); err != nil {
		return err
	}

	delete(publicGroupMap, id)
	return nil
}

// startMixPCM 会议模式混音（独立函数，不是方法）
// 注意：当前版本暂不支持混音功能，未来需要实现 Opus 编解码
func startMixPCM(g *models.Group) {
	pool := getGroupConnPool(g)
	if pool == nil {
		g.ConnPool = &CurrentConnPool{DevConnMap: make(map[string]*models.Device)}
		pool = g.ConnPool.(*CurrentConnPool)
	}

	log.Printf("[MEETING] Group %d - %s: Meeting mode started (audio mixing currently disabled)", g.ID, g.Name)

	// 空闲等待，不做任何处理
	// 未来可实现 Opus 编解码进行混音
	select {}
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
