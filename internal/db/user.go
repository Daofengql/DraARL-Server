package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"draarl/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// UserRepository 用户数据访问层
type UserRepository struct {
	db *sql.DB
}

// userSelectColumns is deliberately explicit and kept aligned with the
// legacy scanner below.  This package is a compatibility SQL layer while the
// canonical model has gained newer columns over time; SELECT * would make
// every read depend on physical table order and column count.
const userSelectColumns = `id, name, callsign, gird, phone, password,
	birthday, sex, avatar, address, roles, introduction, alarm_msg, status,
	update_time, last_login_time, login_err_times, create_time, openid,
	nickname, pid, last_login_ip, dmrid, mdcid`

const adminLookupQuery = "SELECT id FROM users WHERE name = ? LIMIT 1 FOR UPDATE"

func userSelectQuery(predicate string) string {
	query := "SELECT " + userSelectColumns + " FROM users"
	if strings.TrimSpace(predicate) != "" {
		query += " WHERE " + predicate
	}
	return query
}

// NewUserRepository 创建用户仓库
func NewUserRepository() *UserRepository {
	return &UserRepository{db: Get()}
}

// AddUser 添加用户
func (r *UserRepository) AddUser(user *models.User) error {
	// 【H1 安全修复】密码必须 bcrypt 哈希后落库，禁止明文存储
	hashed, err := hashUserPassword(user.Password)
	if err != nil {
		return err
	}
	query := `INSERT INTO users (name, callsign, phone, password, roles, status, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())`

	result, err := r.db.Exec(query, user.Name, user.CallSign, user.Phone, hashed,
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
	query := userSelectQuery("id = ?")
	return r.scanUser(r.db.QueryRow(query, id))
}

// GetUserByCallSign 通过呼号获取用户
func (r *UserRepository) GetUserByCallSign(callsign string) (*models.User, error) {
	query := userSelectQuery("callsign = ?")
	return r.scanUser(r.db.QueryRow(query, callsign))
}

// GetUserByPhone 通过手机号获取用户
func (r *UserRepository) GetUserByPhone(phone string) (*models.User, error) {
	query := userSelectQuery("phone = ?")
	return r.scanUser(r.db.QueryRow(query, phone))
}

// GetUserByOpenID 通过OpenID获取用户
func (r *UserRepository) GetUserByOpenID(openid string) (*models.User, error) {
	query := userSelectQuery("openid = ?")
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
	query := userSelectQuery("") + " ORDER BY id LIMIT ? OFFSET ?"
	rows, err := r.db.Query(query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	users := make([]*models.User, 0)
	for rows.Next() {
		user, err := r.scanUserFromRows(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users: %w", err)
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
	// 【H1 安全修复】bcrypt 哈希后落库
	hashed, err := hashUserPassword(password)
	if err != nil {
		return err
	}
	query := `UPDATE users SET password = ?, update_time = NOW() WHERE id = ?`
	_, err = r.db.Exec(query, hashed, id)
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
	// 【H1 安全修复】改为按 phone 取回哈希后用 bcrypt 恒定时间比较，
	// 不再使用 SQL 明文比对；同时也消除对 scanUser 列数错位的依赖。
	query := userSelectQuery("phone = ?") + " LIMIT 1"
	user, err := r.scanUser(r.db.QueryRow(query, phone))
	if err != nil {
		return nil, fmt.Errorf("用户名或密码错误")
	}
	if !verifyUserPassword(user.Password, password) {
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
	var dmrid sql.NullInt64
	var mdcid sql.NullString

	err := row.Scan(&user.ID, &user.Name, &callSign, &gird, &phone, &user.Password,
		&birthday, &sex, &avatar, &address, &rolesStr, &introduction,
		&alarmMsg, &user.Status, &updateTime, &lastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &openid, &user.NickName, &pid, &lastLoginIP,
		&dmrid, &mdcid)

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
	if dmrid.Valid {
		user.DMRID = uint32(dmrid.Int64)
	}
	if mdcid.Valid {
		user.MDCID = mdcid.String
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
	var dmrid sql.NullInt64
	var mdcid sql.NullString

	err := rows.Scan(&user.ID, &user.Name, &callSign, &gird, &phone, &user.Password,
		&birthday, &sex, &avatar, &address, &rolesStr, &introduction,
		&alarmMsg, &user.Status, &updateTime, &lastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &openid, &user.NickName, &pid, &lastLoginIP,
		&dmrid, &mdcid)

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
	if dmrid.Valid {
		user.DMRID = uint32(dmrid.Int64)
	}
	if mdcid.Valid {
		user.MDCID = mdcid.String
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
	rolesStr = strings.TrimSpace(rolesStr)
	if rolesStr == "" {
		return []string{"user"}
	}
	if strings.HasPrefix(rolesStr, "[") {
		var parsed []string
		if err := json.Unmarshal([]byte(rolesStr), &parsed); err == nil {
			roles := make([]string, 0, len(parsed))
			for _, role := range parsed {
				if role = strings.TrimSpace(role); role != "" {
					roles = append(roles, role)
				}
			}
			if len(roles) > 0 {
				return roles
			}
			return []string{"user"}
		}
		// A malformed JSON-looking value must not become an arbitrary role.
		return []string{"user"}
	}
	// Legacy comma-separated values remain supported for old databases.
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
	query := userSelectQuery("name = ?") + " LIMIT 1"
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
	// 【H1 安全修复】bcrypt 哈希后落库
	hashed, err := hashUserPassword(user.Password)
	if err != nil {
		return err
	}
	result, err := Get().Exec(query, user.Name, hashed, user.NickName, user.Status, roles, now, now)
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
	var dmrid sql.NullInt64
	var mdcid sql.NullString

	err := row.Scan(&user.ID, &user.Name, &callSign, &gird, &phone, &user.Password,
		&birthday, &sex, &avatar, &address, &rolesStr, &introduction,
		&alarmMsg, &user.Status, &updateTime, &lastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &openid, &user.NickName, &pid, &lastLoginIP,
		&dmrid, &mdcid)

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
	if dmrid.Valid {
		user.DMRID = uint32(dmrid.Int64)
	}
	if mdcid.Valid {
		user.MDCID = mdcid.String
	}

	// 解析角色
	user.Roles = deserializeRoles(rolesStr.String)

	return user, nil
}

// hashUserPassword 对用户密码做 bcrypt 哈希；空密码保持为空（允许无密码账号）。
func hashUserPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("密码哈希失败: %w", err)
	}
	return string(hashed), nil
}

// verifyUserPassword 使用 bcrypt 恒定时间比较校验密码哈希。
func verifyUserPassword(hashed, plain string) bool {
	if hashed == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

// ==================== 初始化管理员 ====================

// InitAdminUser 初始化管理员用户（如果不存在）
func InitAdminUser() (string, string, error) {
	// Keep the existence check and insert on one transaction/connection.  A
	// process can otherwise pass a COUNT check concurrently with another
	// startup and turn a normal idempotent initialization into a duplicate-key
	// failure (or duplicate admins on an old schema without the unique index).
	tx, err := Get().Begin()
	if err != nil {
		return "", "", fmt.Errorf("开启管理员初始化事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var existingID int
	err = tx.QueryRow(adminLookupQuery, "admin").Scan(&existingID)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return "", "", fmt.Errorf("确认管理员已存在失败: %w", err)
		}
		return "", "", nil // 已存在 admin 用户，无需创建
	}
	if err != sql.ErrNoRows {
		return "", "", fmt.Errorf("检查管理员用户失败: %w", err)
	}

	// 生成随机密码
	password, err := generateRandomPassword(12)
	if err != nil {
		return "", "", fmt.Errorf("生成密码失败: %w", err)
	}

	// 哈希密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("密码哈希失败: %w", err)
	}

	// 创建默认管理员（已审核通过，拥有完整权限）
	// 角色使用单角色系统：直接存储 "admin" 而不是 ["admin"]
	// 审核状态设为 1（已通过）
	query := `INSERT INTO users (name, password, nickname, status, roles, approval_status, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, 1, NOW(), NOW())`
	_, err = tx.Exec(query, "admin", string(hashedPassword), "系统管理员", 1, "admin")
	if err != nil {
		return "", "", fmt.Errorf("创建管理员失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("提交管理员初始化失败: %w", err)
	}

	return "admin", password, nil
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
