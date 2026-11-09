package db

import (
	"database/sql"
	"log"

	"draarl/internal/models"
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
	query := `INSERT INTO public_groups (name, type, password,
		ower_id, dev_list, status, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())`

	result, err := r.db.Exec(query, group.Name, group.Type, group.Password,
		group.OwerID, group.DevList, group.Status)
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
	query := `SELECT id, name, type, password,
		ower_id, dev_list, status,
		create_time, update_time, note FROM public_groups WHERE id = ?`
	return r.scanGroup(r.db.QueryRow(query, id))
}

// ListPublicGroups 列出所有公共群组
func (r *GroupRepository) ListPublicGroups() ([]*models.Group, error) {
	query := `SELECT id, name, type, password,
		ower_id, dev_list, status,
		create_time, update_time, note FROM public_groups ORDER BY id`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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
	query := `UPDATE public_groups SET name = ?, type = ?, password = ?,
		update_time = NOW() WHERE id = ?`

	_, err := r.db.Exec(query, group.Name, group.Type, group.Password,
		group.ID)
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

	var nullPassword, nullNote, nullDevList sql.NullString

	err := row.Scan(&group.ID, &group.Name, &group.Type, &nullPassword,
		&group.OwerID, &nullDevList, &group.Status,
		&group.CreateTime, &group.UpdateTime, &nullNote)

	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字符串字段
	if nullPassword.Valid {
		group.Password = nullPassword.String
	}
	if nullNote.Valid {
		group.Note = nullNote.String
	}
	// DevList 在数据库中是 TEXT 类型 JSON，保持为空切片
	group.DevList = []int{}

	// 初始化运行时字段
	group.DevMap = make(map[int]*models.Device)

	return group, nil
}

// scanGroupFromRows 从结果集扫描群组
func (r *GroupRepository) scanGroupFromRows(rows *sql.Rows) (*models.Group, error) {
	group := &models.Group{}

	var nullPassword, nullNote, nullDevList sql.NullString

	err := rows.Scan(&group.ID, &group.Name, &group.Type, &nullPassword,
		&group.OwerID, &nullDevList, &group.Status,
		&group.CreateTime, &group.UpdateTime, &nullNote)

	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字符串字段
	if nullPassword.Valid {
		group.Password = nullPassword.String
	}
	if nullNote.Valid {
		group.Note = nullNote.String
	}
	// DevList 在数据库中是 TEXT 类型 JSON，保持为空切片
	group.DevList = []int{}

	// 初始化运行时字段
	group.DevMap = make(map[int]*models.Device)

	return group, nil
}

// RelayRepository 中继台数据访问层
type RelayRepository struct {
	db *sql.DB
}

const relayListQuery = `SELECT id, name, up_freq, down_freq, send_ctss, recive_ctss,
	ower_callsign, create_time, update_time, status, note
	FROM relay WHERE status = 1 ORDER BY id`

// NewRelayRepository 创建中继台仓库
func NewRelayRepository() *RelayRepository {
	return &RelayRepository{db: Get()}
}

// ListRelays 列出所有中继台
func (r *RelayRepository) ListRelays() ([]models.Relay, error) {
	query := relayListQuery
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	relays := []models.Relay{}
	for rows.Next() {
		var relay models.Relay
		err := rows.Scan(&relay.ID, &relay.Name, &relay.UpFreq, &relay.DownFreq,
			&relay.SendCTSS, &relay.ReceiveCTSS, &relay.OwerCallSign,
			&relay.CreateTime, &relay.UpdateTime, &relay.Status, &relay.Note)
		if err != nil {
			return nil, err
		}
		relays = append(relays, relay)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return relays, nil
}

// AddRelay 添加中继台
func (r *RelayRepository) AddRelay(relay *models.Relay) error {
	query := `INSERT INTO relay (name, up_freq, down_freq, send_ctss, recive_ctss, ower_callsign, status, note, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`

	_, err := r.db.Exec(query, relay.Name, relay.UpFreq, relay.DownFreq, relay.SendCTSS, relay.ReceiveCTSS, relay.OwerCallSign, relay.Status, relay.Note)
	if err != nil {
		log.Println("add relay failed, ", err)
		return err
	}

	return nil
}

// UpdateRelay 更新中继台
func (r *RelayRepository) UpdateRelay(relay *models.Relay) error {
	_, err := r.db.Exec(`UPDATE relay SET name=?, up_freq=?, down_freq=?, send_ctss=?, recive_ctss=?, status=?, note=?, update_time=NOW() WHERE id=?`,
		relay.Name, relay.UpFreq, relay.DownFreq, relay.SendCTSS, relay.ReceiveCTSS, relay.Status, relay.Note, relay.ID)
	if err != nil {
		log.Println("update relay failed, ", err)
		return err
	}

	return nil
}

// DeleteRelay 删除中继台
func (r *RelayRepository) DeleteRelay(id int) error {
	_, err := r.db.Exec(`DELETE FROM relay WHERE id=?`, id)
	if err != nil {
		log.Println("delete relay failed, ", err)
		return err
	}

	return nil
}

// GetRelay 获取单个中继台
func (r *RelayRepository) GetRelay(id int) (*models.Relay, error) {
	query := `SELECT id, name, up_freq, down_freq, send_ctss, recive_ctss, ower_callsign, status, note, create_time, update_time FROM relay WHERE id=?`
	row := r.db.QueryRow(query, id)

	relay := &models.Relay{}
	err := row.Scan(&relay.ID, &relay.Name, &relay.UpFreq, &relay.DownFreq, &relay.SendCTSS, &relay.ReceiveCTSS, &relay.OwerCallSign, &relay.Status, &relay.Note, &relay.CreateTime, &relay.UpdateTime)
	if err != nil {
		return nil, err
	}

	return relay, nil
}
