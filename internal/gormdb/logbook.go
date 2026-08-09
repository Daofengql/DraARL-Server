package gormdb

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrRecordNotFound 记录未找到错误
var ErrRecordNotFound = gorm.ErrRecordNotFound

// LogbookRepository 通联日志仓库
type LogbookRepository struct {
	db *gorm.DB
}

// NewLogbookRepository 新建通联日志仓库
func NewLogbookRepository() *LogbookRepository {
	return &LogbookRepository{db: Get()}
}

// Create 创建通联日志
func (r *LogbookRepository) Create(logbook *Logbook) error {
	return r.db.Create(logbook).Error
}

// Update 更新通联日志
func (r *LogbookRepository) Update(logbook *Logbook) error {
	return r.db.Save(logbook).Error
}

// Delete 删除通联日志
func (r *LogbookRepository) Delete(id uint) error {
	return r.db.Delete(&Logbook{}, id).Error
}

// DeleteByUser 删除用户的通联日志（验证所有权）
func (r *LogbookRepository) DeleteByUser(id, userID uint) error {
	result := r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&Logbook{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetByID 通过ID获取通联日志
func (r *LogbookRepository) GetByID(id uint) (*Logbook, error) {
	var logbook Logbook
	err := r.db.First(&logbook, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &logbook, nil
}

// GetByIDAndUser 通过ID和用户ID获取通联日志（验证所有权）
func (r *LogbookRepository) GetByIDAndUser(id, userID uint) (*Logbook, error) {
	var logbook Logbook
	err := r.db.Where("id = ? AND user_id = ?", id, userID).First(&logbook).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &logbook, nil
}

// LogbookQueryParams 查询参数
type LogbookQueryParams struct {
	UserID    uint
	Username  string // 按用户名模糊搜索
	StartTime *time.Time
	EndTime   *time.Time
	CallSign  string
	Frequency float64
	Mode      string
	Page      int
	PageSize  int
}

// List 获取通联日志列表（管理员用，可查看所有）
func (r *LogbookRepository) List(params LogbookQueryParams) ([]*Logbook, int64, error) {
	var logbooks []*Logbook
	var total int64

	offset := (params.Page - 1) * params.PageSize

	// 如果需要按用户名搜索，需要 JOIN users 表
	if params.Username != "" {
		// 使用子查询获取匹配用户名的用户ID列表
		var userIDs []uint
		if err := r.db.Model(&User{}).Where("name LIKE ?", "%"+params.Username+"%").Pluck("id", &userIDs).Error; err != nil {
			return nil, 0, err
		}
		if len(userIDs) == 0 {
			// 没有匹配的用户，返回空结果
			return []*Logbook{}, 0, nil
		}
		paramsCopy := params
		paramsCopy.Username = "" // 清空避免在 applyFilters 中重复处理
		// 使用 userIDs 进行筛选
		query := r.db.Model(&Logbook{}).Where("user_id IN ?", userIDs)
		query = r.applyFilters(query, paramsCopy)

		if err := query.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		if err := query.Order("time_utc DESC").Limit(params.PageSize).Offset(offset).Find(&logbooks).Error; err != nil {
			return nil, 0, err
		}
	} else {
		query := r.db.Model(&Logbook{})
		query = r.applyFilters(query, params)

		if err := query.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		if err := query.Order("time_utc DESC").Limit(params.PageSize).Offset(offset).Find(&logbooks).Error; err != nil {
			return nil, 0, err
		}
	}

	return logbooks, total, nil
}

// ListByUser 获取用户的通联日志列表
func (r *LogbookRepository) ListByUser(params LogbookQueryParams) ([]*Logbook, int64, error) {
	var logbooks []*Logbook
	var total int64

	offset := (params.Page - 1) * params.PageSize
	query := r.db.Model(&Logbook{}).Where("user_id = ?", params.UserID)

	// 应用筛选条件
	query = r.applyFilters(query, params)

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("time_utc DESC").Limit(params.PageSize).Offset(offset).Find(&logbooks).Error; err != nil {
		return nil, 0, err
	}

	return logbooks, total, nil
}

// applyFilters 应用筛选条件
func (r *LogbookRepository) applyFilters(query *gorm.DB, params LogbookQueryParams) *gorm.DB {
	if params.UserID > 0 {
		query = query.Where("user_id = ?", params.UserID)
	}
	if params.StartTime != nil {
		query = query.Where("time_utc >= ?", params.StartTime)
	}
	if params.EndTime != nil {
		query = query.Where("time_utc <= ?", params.EndTime)
	}
	if params.CallSign != "" {
		query = query.Where("callsign LIKE ?", "%"+params.CallSign+"%")
	}
	if params.Frequency > 0 {
		// 频率匹配，允许 1kHz 容差
		tolerance := 0.001
		query = query.Where(
			"ABS(tx_frequency - ?) <= ? OR ABS(rx_frequency - ?) <= ?",
			params.Frequency, tolerance, params.Frequency, tolerance,
		)
	}
	if params.Mode != "" {
		query = query.Where("mode = ?", params.Mode)
	}
	return query
}

// CountByUser 统计用户的通联日志数量
func (r *LogbookRepository) CountByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&Logbook{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// CountAll 统计所有通联日志数量
func (r *LogbookRepository) CountAll() (int64, error) {
	var count int64
	err := r.db.Model(&Logbook{}).Count(&count).Error
	return count, err
}

// BatchDelete 批量删除通联日志
func (r *LogbookRepository) BatchDelete(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Delete(&Logbook{}, ids).Error
}

// BatchDeleteByUser 批量删除用户的通联日志（验证所有权）
func (r *LogbookRepository) BatchDeleteByUser(ids []uint, userID uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.Where("id IN ? AND user_id = ?", ids, userID).Delete(&Logbook{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
