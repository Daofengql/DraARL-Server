package db

import (
	"database/sql"
	"fmt"
	"log"

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
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`

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
	query := `UPDATE users SET name = ?, avatar = ?, introduction = ?, update_time = datetime('now')
		WHERE id = ?`

	_, err := r.db.Exec(query, user.Name, user.Avatar, user.Introduction, user.ID)
	return err
}

// UpdateUserPassword 更新用户密码
func (r *UserRepository) UpdateUserPassword(id int, password string) error {
	query := `UPDATE users SET password = ?, update_time = datetime('now') WHERE id = ?`
	_, err := r.db.Exec(query, password, id)
	return err
}

// UpdateUserAvatar 更新用户头像
func (r *UserRepository) UpdateUserAvatar(user *models.User) error {
	query := `UPDATE users SET avatar = ?, update_time = datetime('now') WHERE id = ?`
	_, err := r.db.Exec(query, user.Avatar, user.ID)
	return err
}

// UpdateUserOpenID 更新用户OpenID
func (r *UserRepository) UpdateUserOpenID(id int, openid string) error {
	query := `UPDATE users SET openid = ?, update_time = datetime('now') WHERE id = ?`
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
	r.db.Exec(`UPDATE users SET last_login_time = datetime('now'), login_err_times = 0 WHERE id = ?`, user.ID)

	return user, nil
}

// AddOperatorLog 添加操作日志
func (r *UserRepository) AddOperatorLog(content, eventType string, operator *models.User) error {
	query := `INSERT INTO operator_log (timestamp, content, event_type, operator, operator_id)
		VALUES (datetime('now'), ?, ?, ?, ?)`

	_, err := r.db.Exec(query, content, eventType, operator.CallSign, operator.ID)
	return err
}

// scanUser 扫描用户行
func (r *UserRepository) scanUser(row *sql.Row) (*models.User, error) {
	user := &models.User{}
	var rolesStr sql.NullString

	err := row.Scan(&user.ID, &user.Name, &user.CallSign, new(string), &user.Phone, &user.Password,
		&user.Birthday, &user.Sex, &user.Avatar, &user.Address, &rolesStr, &user.Introduction,
		&user.AlarmMsg, &user.Status, &user.UpdateTime, &user.LastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &user.OpenID, &user.NickName, new(string), &user.LastLoginIP,
		new(int), new(string))

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, err
	}

	user.Roles = deserializeRoles(rolesStr.String)
	return user, nil
}

// scanUserFromRows 从结果集扫描用户
func (r *UserRepository) scanUserFromRows(rows *sql.Rows) (*models.User, error) {
	user := &models.User{}
	var rolesStr sql.NullString

	err := rows.Scan(&user.ID, &user.Name, &user.CallSign, new(string), &user.Phone, &user.Password,
		&user.Birthday, &user.Sex, &user.Avatar, &user.Address, &rolesStr, &user.Introduction,
		&user.AlarmMsg, &user.Status, &user.UpdateTime, &user.LastLoginTime, &user.LoginErrTimes,
		&user.CreateTime, &user.OpenID, &user.NickName, new(string), &user.LastLoginIP,
		new(int), new(string))

	if err != nil {
		return nil, err
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
