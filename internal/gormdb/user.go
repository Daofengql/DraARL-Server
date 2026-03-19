package gormdb

import (
	"errors"
	"gorm.io/gorm"
)

// UserRepository 用户仓库
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户仓库
func NewUserRepository() *UserRepository {
	return &UserRepository{db: Get()}
}

// ListUsers 获取用户列表
func (r *UserRepository) ListUsers(limit, page int) ([]*User, int64, error) {
	var users []*User
	var total int64

	offset := (page - 1) * limit

	// 获取总数
	if err := r.db.Model(&User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := r.db.Order("id DESC").Limit(limit).Offset(offset).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// GetUserByID 通过ID获取用户
func (r *UserRepository) GetUserByID(id int) (*User, error) {
	var user User
	err := r.db.First(&user, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByName 通过用户名获取用户
func (r *UserRepository) GetUserByName(name string) (*User, error) {
	var user User
	err := r.db.Where("name = ?", name).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByCallSign 通过呼号获取用户
func (r *UserRepository) GetUserByCallSign(callsign string) (*User, error) {
	var user User
	err := r.db.Where("callsign = ?", callsign).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByPhone 通过手机号获取用户
func (r *UserRepository) GetUserByPhone(phone string) (*User, error) {
	var user User
	err := r.db.Where("phone = ?", phone).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByOpenID 通过OpenID获取用户
func (r *UserRepository) GetUserByOpenID(openid string) (*User, error) {
	var user User
	err := r.db.Where("openid = ?", openid).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// FindUserBySSOID 通过SSO提供商和ID查找用户（支持前缀格式 ky:xxx）
// OpenID字段格式: "ky:abc123" 或 "ky:abc123,wx:def456"
func (r *UserRepository) FindUserBySSOID(provider, ssoID string) (*User, error) {
	var user User
	// 使用 LIKE 查询匹配前缀格式
	pattern := provider + ":" + ssoID
	// 需要匹配三种情况：
	// 1. openid = "ky:abc123" (唯一绑定)
	// 2. openid LIKE "ky:abc123,%" (开头)
	// 3. openid LIKE "%,ky:abc123,%" (中间)
	// 4. openid LIKE "%,ky:abc123" (结尾)
	err := r.db.Where("openid = ? OR openid LIKE ? OR openid LIKE ? OR openid LIKE ?",
		pattern,
		pattern+",%",
		"%,"+pattern+",%",
		"%,"+pattern,
	).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// CreateUser 创建用户
func (r *UserRepository) CreateUser(user *User) error {
	return r.db.Create(user).Error
}

// UpdateUser 更新用户基本信息
func (r *UserRepository) UpdateUser(user *User) error {
	return r.db.Model(user).Updates(user).Error
}

// UpdateUserOpenID 更新用户OpenID（解决GORM零值不更新问题）
func (r *UserRepository) UpdateUserOpenID(id int, openID string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("openid", openID).Error
}

// UpdateUserPassword 更新用户密码
func (r *UserRepository) UpdateUserPassword(id int, password string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("password", password).Error
}

// UpdateUserAvatar 更新用户头像
func (r *UserRepository) UpdateUserAvatar(id int, avatar string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("avatar", avatar).Error
}

// UpdateUserCallSign 更新用户呼号
func (r *UserRepository) UpdateUserCallSign(id int, callsign string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("callsign", callsign).Error
}

// UpdateUserRoles 更新用户角色
func (r *UserRepository) UpdateUserRoles(id int, roles string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("roles", roles).Error
}

// UpdateUserStatus 更新用户状态
func (r *UserRepository) UpdateUserStatus(id int, status int) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("status", status).Error
}

// UpdateLastLogin 更新最后登录时间和IP
func (r *UserRepository) UpdateLastLogin(userID int, ip string) error {
	return r.db.Model(&User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"last_login_time":  gorm.Expr("NOW()"),
		"last_login_ip":    ip,
		"login_err_times":  0,
	}).Error
}

// IncrementLoginError 增加登录错误次数
func (r *UserRepository) IncrementLoginError(userID int) error {
	return r.db.Model(&User{}).Where("id = ?", userID).UpdateColumn("login_err_times", gorm.Expr("login_err_times + 1")).Error
}

// DeleteUser 删除用户
func (r *UserRepository) DeleteUser(id int) error {
	return r.db.Delete(&User{}, id).Error
}

// AddOperatorLog 添加操作日志
func (r *UserRepository) AddOperatorLog(log *OperatorLog) error {
	return r.db.Create(log).Error
}

// UserCount 获取用户总数
func (r *UserRepository) UserCount() (int64, error) {
	var count int64
	err := r.db.Model(&User{}).Count(&count).Error
	return count, err
}

// AdminUserCount 获取管理员用户数量
func (r *UserRepository) AdminUserCount() (int64, error) {
	var count int64
	err := r.db.Model(&User{}).Where("roles LIKE ?", "%admin%").Count(&count).Error
	return count, err
}

// ListByApprovalStatus 根据审核状态获取用户列表
func (r *UserRepository) ListByApprovalStatus(status int, limit, offset int) ([]*User, error) {
	var users []*User
	err := r.db.Where("approval_status = ?", status).
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&users).Error
	return users, err
}

// CountByApprovalStatus 统计指定审核状态的用户数量
func (r *UserRepository) CountByApprovalStatus(status int) (int64, error) {
	var count int64
	err := r.db.Model(&User{}).Where("approval_status = ?", status).Count(&count).Error
	return count, err
}

// HasAdminUser 检查是否存在管理员用户
func (r *UserRepository) HasAdminUser() bool {
	var count int64
	r.db.Model(&User{}).Where("roles LIKE ?", "%admin%").Count(&count)
	return count > 0
}

// SearchUsers 搜索用户（按用户名或呼号）
func (r *UserRepository) SearchUsers(keyword string, limit, page int) ([]*User, int64, error) {
	var users []*User
	var total int64

	offset := (page - 1) * limit
	query := r.db.Model(&User{}).Where("name LIKE ? OR callsign LIKE ?", "%"+keyword+"%", "%"+keyword+"%")

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// UpdateUserApproval 更新用户审核状态
func (r *UserRepository) UpdateUserApproval(id int, status int, reviewerID int, note string) error {
	updates := map[string]interface{}{
		"approval_status": status,
		"reviewer_id":     reviewerID,
		"review_note":     note,
		"review_time":     gorm.Expr("NOW()"),
	}
	return r.db.Model(&User{}).Where("id = ?", id).Updates(updates).Error
}

// GetPendingUsers 获取待审核用户列表
func (r *UserRepository) GetPendingUsers(limit, page int) ([]*User, int64, error) {
	var users []*User
	var total int64

	offset := (page - 1) * limit

	// 获取总数
	if err := r.db.Model(&User{}).Where("approval_status = ?", 0).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	if err := r.db.Where("approval_status = ?", 0).
		Order("create_time DESC").
		Limit(limit).
		Offset(offset).
		Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// UpdateUserDevicePassword 更新用户设备准入密码
func (r *UserRepository) UpdateUserDevicePassword(id int, devicePassword string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("device_password", devicePassword).Error
}

// GetUserDevicePassword 获取用户设备密码哈希
func (r *UserRepository) GetUserDevicePassword(id int) (string, error) {
	var user User
	err := r.db.Select("device_password").First(&user, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return user.DevicePassword, nil
}

// UpdateLastGroupID 更新用户最后选中的群组ID
func (r *UserRepository) UpdateLastGroupID(userID int, groupID int) error {
	return r.db.Model(&User{}).Where("id = ?", userID).Update("last_group_id", groupID).Error
}

// GetApprovedUserCount 获取已审核通过的用户总数
func (r *UserRepository) GetApprovedUserCount() (int64, error) {
	var count int64
	err := r.db.Model(&User{}).Where("status = 1 AND approval_status = 1").Count(&count).Error
	return count, err
}

// GetUsersByIDs 批量获取用户信息（用于解决 N+1 查询问题）
func (r *UserRepository) GetUsersByIDs(ids []int) ([]*User, error) {
	if len(ids) == 0 {
		return []*User{}, nil
	}
	var users []*User
	err := r.db.Where("id IN ?", ids).Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

// UserBriefInfo 用户简要信息（用于关联查询）
type UserBriefInfo struct {
	ID       int    `json:"id"`
	CallSign string `json:"callsign"`
	NickName string `json:"nickname"`
	Name     string `json:"name"`
}

// GetUserBriefByIDs 批量获取用户简要信息（只查询必要字段）
func (r *UserRepository) GetUserBriefByIDs(ids []int) (map[int]*UserBriefInfo, error) {
	if len(ids) == 0 {
		return make(map[int]*UserBriefInfo), nil
	}
	var users []UserBriefInfo
	err := r.db.Model(&User{}).Select("id, callsign, nickname, name").Where("id IN ?", ids).Find(&users).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int]*UserBriefInfo, len(users))
	for i := range users {
		result[users[i].ID] = &users[i]
	}
	return result, nil
}
