package udphub

import (
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
)

func newConnPool() *CurrentConnPool {
	pool := &CurrentConnPool{DevConnMap: make(map[string]*models.Device)}
	pool.storeConnList(make([]*models.Device, 0))
	return pool
}

// initPublicGroups 初始化公共群组
func initPublicGroups() {
	// 创建全网通群组 999
	publicGroupMap[models.GroupIDPublicMin] = &models.Group{
		ID:         models.GroupIDPublicMin,
		Name:       "全网互联",
		Type:       models.GroupTypeRelay,
		Status:     1,
		DevMap:     make(map[int]*models.Device),
		CreateTime: time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		ConnPool:   newConnPool(),
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
		newGroup.ConnPool = newConnPool()
		newGroup.DevMap = make(map[int]*models.Device)

		publicGroupMap[newGroup.ID] = newGroup

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
		ID:           gp.ID,
		Name:         gp.Name,
		Type:         gp.Type,
		Password:     gp.Password,
		OwerID:       gp.OwerID,
		DevList:      gp.DevList,
		MasterServer: gp.MasterServer,
		SlaveServer:  gp.SlaveServer,
		Status:       1,
		CreateTime:   time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime:   time.Now().Format("2006-01-02 15:04:05"),
		Note:         gp.Note,
		ConnPool:     newConnPool(),
		DevMap:       make(map[int]*models.Device),
	}

	publicGroupMap[newGroup.ID] = newGroup

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
		existing.Password = gp.Password
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
	if gp, ok := GetGroupFromCache(groupID); ok && gp != nil {
		groupRuntimeMu.RLock()
		for _, dev := range gp.DevMap {
			if dev.ISOnline {
				devices = append(devices, dev)
			}
		}
		groupRuntimeMu.RUnlock()
	}

	return devices
}

// GetAllDevicesByGroup 获取群组的所有设备
func GetAllDevicesByGroup(groupID int) []*models.Device {
	devices := make([]*models.Device, 0)
	if gp, ok := GetGroupFromCache(groupID); ok && gp != nil {
		groupRuntimeMu.RLock()
		for _, dev := range gp.DevMap {
			devices = append(devices, dev)
		}
		groupRuntimeMu.RUnlock()
	}

	return devices
}

// GroupStats 群组统计信息
type GroupStats struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Type            int    `json:"type"`
	OwnerID         int    `json:"-"`
	OnlineDevNumber int    `json:"online_dev_number"`
	TotalDevNumber  int    `json:"total_dev_number"`
}

// GetAllGroupStats 获取所有群组统计信息
func GetAllGroupStats() []GroupStats {
	statsByID := make(map[int]GroupStats)
	storeStat := func(gp *models.Group) {
		if gp == nil || gp.IsVirtual || (gp.Type != 1 && gp.Type != 2) {
			return
		}
		groupRuntimeMu.RLock()
		stat := GroupStats{
			ID:              gp.ID,
			Name:            gp.Name,
			Type:            gp.Type,
			OwnerID:         gp.OwerID,
			OnlineDevNumber: gp.OnlineDevNumber,
			TotalDevNumber:  gp.TotalDevNumber,
		}
		groupRuntimeMu.RUnlock()
		if existing, ok := statsByID[gp.ID]; ok {
			// 同一私有群组可能同时出现在全局缓存和用户运行时列表中。
			// 取较新的最大计数，避免重复返回且不把重复副本相加。
			if existing.OnlineDevNumber > stat.OnlineDevNumber {
				stat.OnlineDevNumber = existing.OnlineDevNumber
			}
			if existing.TotalDevNumber > stat.TotalDevNumber {
				stat.TotalDevNumber = existing.TotalDevNumber
			}
		}
		statsByID[gp.ID] = stat
	}

	// 全局群组缓存。
	for _, gp := range GetAllGroupsFromCache() {
		storeStat(gp)
	}

	// 用户运行时私有群组。
	userList.Range(func(k, v any) bool {
		u := v.(*UserInfo)
		for _, gp := range u.Groups {
			storeStat(gp)
		}
		return true
	})

	ids := make([]int, 0, len(statsByID))
	for id := range statsByID {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	stats := make([]GroupStats, 0, len(ids))
	for _, id := range ids {
		stats = append(stats, statsByID[id])
	}
	return stats
}
