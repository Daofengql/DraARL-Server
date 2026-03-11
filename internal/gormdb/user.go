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

// CreateUser 创建用户
func (r *UserRepository) CreateUser(user *User) error {
	return r.db.Create(user).Error
}

// UpdateUser 更新用户基本信息
func (r *UserRepository) UpdateUser(user *User) error {
	return r.db.Model(user).Updates(user).Error
}

// UpdateUserPassword 更新用户密码
func (r *UserRepository) UpdateUserPassword(id int, password string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("password", password).Error
}

// UpdateUserAvatar 更新用户头像
func (r *UserRepository) UpdateUserAvatar(id int, avatar string) error {
	return r.db.Model(&User{}).Where("id = ?", id).Update("avatar", avatar).Error
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
