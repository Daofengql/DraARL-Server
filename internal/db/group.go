package db

import (
	"database/sql"
	"log"

	"nrllink/internal/models"
)

// GroupRepository 群组数据访问层
type GroupRepository struct {
	db *sql.DB
}

// NewGroupRepository 创建群组仓库
func NewGroupRepository() *GroupRepository {
	return &GroupRepository{db: Get()}
}

// AddPublicGroup 添加公共群组
func (r *GroupRepository) AddPublicGroup(group *models.Group) error {
	query := `INSERT INTO public_groups (name, type, callsign, password, allow_callsign_ssid,
		ower_id, ower_callsign, status, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`

	result, err := r.db.Exec(query, group.Name, group.Type, group.CallSign, group.Password,
		group.AllowCallSignSSID, group.OwerID, group.OwerCallSign, group.Status)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}

	group.ID = int(id)
	return nil
}

// GetPublicGroup 获取公共群组
func (r *GroupRepository) GetPublicGroup(id int) (*models.Group, error) {
	query := `SELECT * FROM public_groups WHERE id = ?`
	return r.scanGroup(r.db.QueryRow(query, id))
}

// ListPublicGroups 列出所有公共群组
func (r *GroupRepository) ListPublicGroups() ([]*models.Group, error) {
	query := `SELECT * FROM public_groups ORDER BY id`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]*models.Group, 0)
	for rows.Next() {
		group, err := r.scanGroupFromRows(rows)
		if err != nil {
			log.Printf("Error scanning group: %v", err)
			continue
		}
		groups = append(groups, group)
	}

	return groups, nil
}

// UpdatePublicGroup 更新公共群组
func (r *GroupRepository) UpdatePublicGroup(group *models.Group) error {
	query := `UPDATE public_groups SET name = ?, type = ?, password = ?, allow_callsign_ssid = ?,
		update_time = datetime('now') WHERE id = ?`

	_, err := r.db.Exec(query, group.Name, group.Type, group.Password,
		group.AllowCallSignSSID, group.ID)
	return err
}

// DeletePublicGroup 删除公共群组
func (r *GroupRepository) DeletePublicGroup(id int) error {
	query := `DELETE FROM public_groups WHERE id = ?`
	_, err := r.db.Exec(query, id)
	return err
}

// scanGroup 扫描群组行
func (r *GroupRepository) scanGroup(row *sql.Row) (*models.Group, error) {
	group := &models.Group{}

	err := row.Scan(&group.ID, &group.Name, &group.Type, &group.CallSign, &group.Password,
		new(string), &group.AllowCallSignSSID, &group.OwerID, &group.DevList,
		&group.MasterServer, &group.SlaveServer, &group.Status,
		&group.CreateTime, &group.UpdateTime, &group.Note)

	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	// 初始化运行时字段
	group.DevMap = make(map[int]*models.Device)

	return group, nil
}

// scanGroupFromRows 从结果集扫描群组
func (r *GroupRepository) scanGroupFromRows(rows *sql.Rows) (*models.Group, error) {
	group := &models.Group{}

	err := rows.Scan(&group.ID, &group.Name, &group.Type, &group.CallSign, &group.Password,
		new(string), &group.AllowCallSignSSID, &group.OwerID, &group.DevList,
		&group.MasterServer, &group.SlaveServer, &group.Status,
		&group.CreateTime, &group.UpdateTime, &group.Note)

	if err != nil {
		return nil, err
	}

	// 初始化运行时字段
	group.DevMap = make(map[int]*models.Device)

	return group, nil
}

// RelayRepository 中继台数据访问层
type RelayRepository struct {
	db *sql.DB
}

// NewRelayRepository 创建中继台仓库
func NewRelayRepository() *RelayRepository {
	return &RelayRepository{db: Get()}
}

// ListRelays 列出所有中继台
func (r *RelayRepository) ListRelays() ([]models.Relay, error) {
	query := `SELECT * FROM relay WHERE status = 1`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relays := []models.Relay{}
	for rows.Next() {
		var relay models.Relay
		err := rows.Scan(&relay.ID, &relay.Name, &relay.UpFreq, &relay.DownFreq,
			&relay.SendCTSS, &relay.ReceiveCTSS, &relay.OwerCallSign,
			&relay.CreateTime, &relay.UpdateTime, &relay.Status, &relay.Note)
		if err != nil {
			log.Printf("Error scanning relay: %v", err)
			continue
		}
		relays = append(relays, relay)
	}

	return relays, nil
}

// ServerRepository 服务器数据访问层
type ServerRepository struct {
	db *sql.DB
}

// NewServerRepository 创建服务器仓库
func NewServerRepository() *ServerRepository {
	return &ServerRepository{db: Get()}
}

// ListServers 列出所有服务器
func (r *ServerRepository) ListServers() ([]*models.Server, error) {
	query := `SELECT * FROM servers WHERE status = 1`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := []*models.Server{}
	for rows.Next() {
		server := &models.Server{}
		var dnsName sql.NullString
		err := rows.Scan(&server.ID, &server.Name, new(int), new(string), new(string),
			new(string), new(int), new(int), new(string), new(int),
			new(string), &dnsName, new(int), new(string),
			new(bool), &server.Online, &server.CreateTime, &server.UpdateTime,
			new(string), new(int))
		if err != nil {
			log.Printf("Error scanning server: %v", err)
			continue
		}
		if dnsName.Valid {
			server.Host = dnsName.String
		}
		servers = append(servers, server)
	}

	return servers, nil
}
