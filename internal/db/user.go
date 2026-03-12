package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"time"

	"golang.org/x/crypto/bcrypt"
	"nrllink/internal/models"
)

// UserRepository 用户数据访问层
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository 创建用户仓库
func NewUserRepository() *UserRepository {
	return &UserRepository{db: Get()}
}

// AddUser 添加用户
func (r *UserRepository) AddUser(user *models.User) error {
	query := `INSERT INTO users (name, callsign, phone, password, roles, status, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())`

	result, err := r.db.Exec(query, user.Name, user.CallSign, user.Phone, user.Password,
		serializeRoles(user.Roles), user.Status)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	user.ID = int(id)
	return nil
}

// GetUser 获取用户
func (r *UserRepository) GetUser(id int) (*models.User, error) {
	query := `SELECT * FROM users WHERE id = ?`
	return r.scanUser(r.db.QueryRow(query, id))
}

// GetUserByCallSign 通过呼号获取用户
func (r *UserRepository) GetUserByCallSign(callsign string) (*models.User, error) {
	query := `SELECT * FROM users WHERE callsign = ?`
	return r.scanUser(r.db.QueryRow(query, callsign))
}

// GetUserByPhone 通过手机号获取用户
func (r *UserRepository) GetUserByPhone(phone string) (*models.User, error) {
	query := `SELECT * FROM users WHERE phone = ?`
	return r.scanUser(r.db.QueryRow(query, phone))
}

// GetUserByOpenID 通过OpenID获取用户
func (r *UserRepository) GetUserByOpenID(openid string) (*models.User, error) {
	query := `SELECT * FROM users WHERE openid = ?`
	return r.scanUser(r.db.QueryRow(query, openid))
}

// ListUsers 列出所有用户
func (r *UserRepository) ListUsers(limit, page int) ([]*models.User, int, error) {
	offset := (page - 1) * limit

	// 获取总数
	var total int
	err := r.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// 获取分页数据
	query := `SELECT * FROM users ORDER BY id LIMIT ? OFFSET ?`
	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]*models.User, 0)
	for rows.Next() {
		user, err := r.scanUserFromRows(rows)
		if err != nil {
			log.Printf("Error scanning user: %v", err)
			continue
		}
		users = append(users, user)
	}

	return users, total, nil
}

// UpdateUser 更新用户
func (r *UserRepository) UpdateUser(user *models.User) error {
	query := `UPDATE users SET name = ?, avatar = ?, introduction = ?, update_time = NOW()
		WHERE id = ?`

	_, err := r.db.Exec(query, user.Name, user.Avatar, user.Introduction, user.ID)
	return err
}

// UpdateUserPassword 更新用户密码
func (r *UserRepository) UpdateUserPassword(id int, password string) error {
	query := `UPDATE users SET password = ?, update_time = NOW() WHERE id = ?`
	_, err := r.db.Exec(query, password, id)
	return err
}

// UpdateUserAvatar 更新用户头像
func (r *UserRepository) UpdateUserAvatar(user *models.User) error {
	query := `UPDATE users SET avatar = ?, update_time = NOW() WHERE id = ?`
	_, err := r.db.Exec(query, user.Avatar, user.ID)
	return err
}

// UpdateUserOpenID 更新用户OpenID
func (r *UserRepository) UpdateUserOpenID(id int, openid string) error {
	query := `UPDATE users SET openid = ?, update_time = NOW() WHERE id = ?`
	_, err := r.db.Exec(query, openid, id)
	return err
}

// DeleteUser 删除用户
func (r *UserRepository) DeleteUser(id int) error {
	query := `DELETE FROM users WHERE id = ?`
	_, err := r.db.Exec(query, id)
	return err
}

// VerifyPassword 验证用户密码
func (r *UserRepository) VerifyPassword(phone, password string) (*models.User, error) {
	query := `SELECT * FROM users WHERE phone = ? AND password = ?`
	user, err := r.scanUser(r.db.QueryRow(query, phone, password))
	if err != nil {
		return nil, fmt.Errorf("用户名或密码错误")
	}

	// 更新登录时间
	r.db.Exec(`UPDATE users SET last_login_time = NOW(), login_err_times = 0 WHERE id = ?`, user.ID)

	return user, nil
}

// AddOperatorLog 添加操作日志
func (r *UserRepository) AddOperatorLog(content, eventType string, operator *models.User) error {
	query := `INSERT INTO operator_log (timestamp, content, event_type, operator, operator_id)
		VALUES (NOW(), ?, ?, ?, ?)`

	_, err := r.db.Exec(query, content, eventType, operator.CallSign, operator.ID)
	return err
}

// scanUser 扫描用户行
func (r *UserRepository) scanUser(row *sql.Row) (*models.User, error) {
	user := &models.User{}
	var rolesStr, callSign, gird, phone, birthday, avatar, address, introduction, openid, pid, lastLoginIP, updateTime, lastLoginTime sql.NullString
	var sex sql.NullInt64
	var alarmMsg sql.NullBool

	err := row.Scan(&user.ID, &user.Name, &callSign, &gird, &phone, &user.Password,
		&birthday, &sex, &avatar, &address, &rolesStr, &introduction,
		&alarmMsg, &user.Status, &updateTime, &lastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &openid, &user.NickName, &pid, &lastLoginIP,
		new(int), new(string))

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字段
	if callSign.Valid {
		user.CallSign = callSign.String
	}
	if phone.Valid {
		user.Phone = phone.String
	}
	if birthday.Valid {
		user.Birthday = birthday.String
	}
	if avatar.Valid {
		user.Avatar = avatar.String
	}
	if address.Valid {
		user.Address = address.String
	}
	if introduction.Valid {
		user.Introduction = introduction.String
	}
	if updateTime.Valid {
		user.UpdateTime = updateTime.String
	}
	if lastLoginTime.Valid {
		user.LastLoginTime = lastLoginTime.String
	}
	if sex.Valid {
		user.Sex = int(sex.Int64)
	}
	if alarmMsg.Valid {
		user.AlarmMsg = alarmMsg.Bool
	}
	if openid.Valid {
		user.OpenID = openid.String
	}
	if lastLoginIP.Valid {
		user.LastLoginIP = lastLoginIP.String
	}

	user.Roles = deserializeRoles(rolesStr.String)
	return user, nil
}

// scanUserFromRows 从结果集扫描用户
func (r *UserRepository) scanUserFromRows(rows *sql.Rows) (*models.User, error) {
	user := &models.User{}
	var rolesStr, callSign, gird, phone, birthday, avatar, address, introduction, openid, pid, lastLoginIP, updateTime, lastLoginTime sql.NullString
	var sex sql.NullInt64
	var alarmMsg sql.NullBool

	err := rows.Scan(&user.ID, &user.Name, &callSign, &gird, &phone, &user.Password,
		&birthday, &sex, &avatar, &address, &rolesStr, &introduction,
		&alarmMsg, &user.Status, &updateTime, &lastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &openid, &user.NickName, &pid, &lastLoginIP,
		new(int), new(string))

	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字段
	if callSign.Valid {
		user.CallSign = callSign.String
	}
	if phone.Valid {
		user.Phone = phone.String
	}
	if birthday.Valid {
		user.Birthday = birthday.String
	}
	if avatar.Valid {
		user.Avatar = avatar.String
	}
	if address.Valid {
		user.Address = address.String
	}
	if introduction.Valid {
		user.Introduction = introduction.String
	}
	if updateTime.Valid {
		user.UpdateTime = updateTime.String
	}
	if lastLoginTime.Valid {
		user.LastLoginTime = lastLoginTime.String
	}
	if sex.Valid {
		user.Sex = int(sex.Int64)
	}
	if alarmMsg.Valid {
		user.AlarmMsg = alarmMsg.Bool
	}
	if openid.Valid {
		user.OpenID = openid.String
	}
	if lastLoginIP.Valid {
		user.LastLoginIP = lastLoginIP.String
	}

	user.Roles = deserializeRoles(rolesStr.String)
	return user, nil
}

// serializeRoles 序列化角色数组
func serializeRoles(roles []string) string {
	if len(roles) == 0 {
		return ""
	}
	result := "["
	for i, role := range roles {
		if i > 0 {
			result += ","
		}
		result += `"` + role + `"`
	}
	result += "]"
	return result
}

// deserializeRoles 反序列化角色数组
func deserializeRoles(rolesStr string) []string {
	if rolesStr == "" {
		return []string{"user"}
	}
	// 简化处理，实际应该使用JSON解析
	if rolesStr[0] == '[' {
		rolesStr = rolesStr[1 : len(rolesStr)-1]
	}
	// 分割并清理引号
	roles := []string{}
	for _, r := range splitAndTrim(rolesStr, ",") {
		if len(r) > 0 {
			roles = append(roles, r)
		}
	}
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	return roles
}

// splitAndTrim 分割并修剪字符串
func splitAndTrim(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	parts := []string{}
	current := ""
	inQuote := false
	for _, c := range s {
		switch c {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				parts = append(parts, current)
				current = ""
			} else {
				current += string(c)
			}
		default:
			current += string(c)
		}
	}
	parts = append(parts, current)
	return parts
}

// ==================== 包级别函数（供 handler 使用） ====================

// GetUserByUsername 通过用户名获取用户（使用 name 字段）
func GetUserByUsername(username string) (*models.User, error) {
	query := `SELECT * FROM users WHERE name = ? LIMIT 1`
	return scanUserDirect(Get().QueryRow(query, username))
}

// CreateUser 创建用户
func CreateUser(user *models.User) error {
	query := `INSERT INTO users (name, password, nickname, status, roles, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	now := time.Now().Format("2006-01-02 15:04:05")
	// 使用 roles 字段存储角色信息
	roles := "user"
	if len(user.Roles) > 0 {
		roles = serializeRoles(user.Roles)
	}
	result, err := Get().Exec(query, user.Name, user.Password, user.NickName, user.Status, roles, now, now)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	user.ID = int(id)
	return nil
}

// UpdateLastLogin 更新最后登录时间
func UpdateLastLogin(userID int, ip string) error {
	query := `UPDATE users SET last_login_time = ?, last_login_ip = ?, login_err_times = 0 WHERE id = ?`
	_, err := Get().Exec(query, time.Now().Format("2006-01-02 15:04:05"), ip, userID)
	return err
}

// UpdateLoginError 更新登录错误次数
func UpdateLoginError(userID int) error {
	query := `UPDATE users SET login_err_times = login_err_times + 1 WHERE id = ?`
	_, err := Get().Exec(query, userID)
	return err
}

// scanUserDirect 直接扫描用户行（不使用仓库）
func scanUserDirect(row *sql.Row) (*models.User, error) {
	user := &models.User{}
	var rolesStr, callSign, gird, phone, birthday, avatar, address, introduction, openid, pid, lastLoginIP, updateTime, lastLoginTime sql.NullString
	var sex sql.NullInt64
	var alarmMsg sql.NullBool

	err := row.Scan(&user.ID, &user.Name, &callSign, &gird, &phone, &user.Password,
		&birthday, &sex, &avatar, &address, &rolesStr, &introduction,
		&alarmMsg, &user.Status, &updateTime, &lastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &openid, &user.NickName, &pid, &lastLoginIP,
		new(int), new(string))

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字符串字段
	if callSign.Valid {
		user.CallSign = callSign.String
	}
	if phone.Valid {
		user.Phone = phone.String
	}
	if birthday.Valid {
		user.Birthday = birthday.String
	}
	if avatar.Valid {
		user.Avatar = avatar.String
	}
	if address.Valid {
		user.Address = address.String
	}
	if introduction.Valid {
		user.Introduction = introduction.String
	}
	if updateTime.Valid {
		user.UpdateTime = updateTime.String
	}
	if lastLoginTime.Valid {
		user.LastLoginTime = lastLoginTime.String
	}
	if sex.Valid {
		user.Sex = int(sex.Int64)
	}
	if alarmMsg.Valid {
		user.AlarmMsg = alarmMsg.Bool
	}
	if openid.Valid {
		user.OpenID = openid.String
	}
	if lastLoginIP.Valid {
		user.LastLoginIP = lastLoginIP.String
	}

	// 解析角色
	user.Roles = deserializeRoles(rolesStr.String)

	return user, nil
}

// ==================== 初始化管理员 ====================

// InitAdminUser 初始化管理员用户（如果不存在）
func InitAdminUser() (string, string, error) {
	// 检查名为 "admin" 的用户是否已存在
	var count int
	err := Get().QueryRow("SELECT COUNT(*) FROM users WHERE name = 'admin'").Scan(&count)
	if err != nil {
		return "", "", fmt.Errorf("检查管理员用户失败: %w", err)
	}
	if count > 0 {
		return "", "", nil // 已存在 admin 用户，无需创建
	}

	// 生成随机密码
	password, err := generateRandomPassword(12)
	if err != nil {
		return "", "", fmt.Errorf("生成密码失败: %w", err)
	}

	// ���希密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("密码哈希失败: %w", err)
	}

	// 创建默认管理员
	admin := &models.User{
		Name:     "admin",
		Password: string(hashedPassword),
		NickName: "系统管理员",
		Status:   1,
		Roles:    []string{"admin"},
	}

	// 使用 SQL 直接插入（绕过仓库层的角色序列化）
	query := `INSERT INTO users (name, password, nickname, status, roles, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, NOW(), NOW())`
	_, err = Get().Exec(query, admin.Name, admin.Password, admin.NickName, admin.Status, serializeRoles(admin.Roles))
	if err != nil {
		return "", "", fmt.Errorf("创建管理员失败: %w", err)
	}

	return admin.Name, password, nil
}

// generateRandomPassword 生成随机密码
func generateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	result := make([]byte, length)

	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}

	return string(result), nil
}

// HasAdminUser 检查是否存在管理员用户
func HasAdminUser() bool {
	var count int
	err := Get().QueryRow("SELECT COUNT(*) FROM users WHERE roles LIKE '%admin%'").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}
