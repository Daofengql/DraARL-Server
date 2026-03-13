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
	query := `INSERT INTO public_groups (name, type, call_sign, password, allow_callsign_ssid,
		ower_id, devlist, master_server, slave_server, status, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`

	result, err := r.db.Exec(query, group.Name, group.Type, group.CallSign, group.Password,
		group.AllowCallSignSSID, group.OwerID, group.DevList, group.MasterServer,
		group.SlaveServer, group.Status)
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
	query := `SELECT id, name, type, call_sign, password, allow_callsign_ssid,
		ower_id, dev_list, master_server, slave_server, status,
		create_time, update_time, note FROM public_groups WHERE id = ?`
	return r.scanGroup(r.db.QueryRow(query, id))
}

// ListPublicGroups 列出所有公共群组
func (r *GroupRepository) ListPublicGroups() ([]*models.Group, error) {
	query := `SELECT id, name, type, call_sign, password, allow_callsign_ssid,
		ower_id, dev_list, master_server, slave_server, status,
		create_time, update_time, note FROM public_groups ORDER BY id`
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
	query := `UPDATE public_groups SET name = ?, type = ?, call_sign = ?, password = ?, allow_callsign_ssid = ?,
		update_time = NOW() WHERE id = ?`

	_, err := r.db.Exec(query, group.Name, group.Type, group.CallSign, group.Password,
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

	var nullCallSign, nullPassword, nullAllowCallSignSSID, nullNote, nullDevList sql.NullString

	err := row.Scan(&group.ID, &group.Name, &group.Type, &nullCallSign, &nullPassword,
		&nullAllowCallSignSSID, &group.OwerID,
		&nullDevList, &group.MasterServer, &group.SlaveServer, &group.Status,
		&group.CreateTime, &group.UpdateTime, &nullNote)

	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字符串字段
	if nullCallSign.Valid {
		group.CallSign = nullCallSign.String
	}
	if nullPassword.Valid {
		group.Password = nullPassword.String
	}
	if nullAllowCallSignSSID.Valid {
		group.AllowCallSignSSID = nullAllowCallSignSSID.String
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

	var nullCallSign, nullPassword, nullAllowCallSignSSID, nullNote, nullDevList sql.NullString

	err := rows.Scan(&group.ID, &group.Name, &group.Type, &nullCallSign, &nullPassword,
		&nullAllowCallSignSSID, &group.OwerID,
		&nullDevList, &group.MasterServer, &group.SlaveServer, &group.Status,
		&group.CreateTime, &group.UpdateTime, &nullNote)

	if err != nil {
		return nil, err
	}

	// 处理可能为 NULL 的字符串字段
	if nullCallSign.Valid {
		group.CallSign = nullCallSign.String
	}
	if nullPassword.Valid {
		group.Password = nullPassword.String
	}
	if nullAllowCallSignSSID.Valid {
		group.AllowCallSignSSID = nullAllowCallSignSSID.String
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

// AddServer 添加服务器
func (r *ServerRepository) AddServer(server *models.Server) error {
	query := `INSERT INTO servers (name, join_key, cpu_type, mem_size, input_rate, output_rate, netcard,
		ip_type, ip_addr, udp_port, dns_name, server_type, ower_id, ower_callsign, status, note, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`

	_, err := r.db.Exec(query, server.Name, server.JoinKey, server.CpuType, server.MemSize,
		server.InputRate, server.OutputRate, server.NetCard, server.IPType, server.IPAddr,
		server.UDPPort, server.DNSName, server.ServerType, server.OwerID, server.OwerCallSign,
		server.Status, server.Note)
	if err != nil {
		log.Println("add server failed, ", err)
		return err
	}

	return nil
}

// UpdateServer 更新服务器
func (r *ServerRepository) UpdateServer(server *models.Server) error {
	_, err := r.db.Exec(`UPDATE servers SET name=?, cpu_type=?, mem_size=?, input_rate=?, output_rate=?, netcard=?,
		ip_type=?, ip_addr=?, udp_port=?, dns_name=?, server_type=?, ower_id=?, ower_callsign=?, status=?, note=?, join_key=?, update_time=NOW()
		WHERE id=?`,
		server.Name, server.CpuType, server.MemSize, server.InputRate, server.OutputRate,
		server.NetCard, server.IPType, server.IPAddr, server.UDPPort, server.DNSName,
		server.ServerType, server.OwerID, server.OwerCallSign, server.Status, server.Note,
		server.JoinKey, server.ID)
	if err != nil {
		log.Println("update server failed, ", err)
		return err
	}

	return nil
}

// DeleteServer 删除服务器
func (r *ServerRepository) DeleteServer(id int) error {
	_, err := r.db.Exec(`DELETE FROM servers WHERE id=?`, id)
	if err != nil {
		log.Println("delete server failed, ", err)
		return err
	}

	return nil
}

// GetServer 获取单个服务器
func (r *ServerRepository) GetServer(id int) (*models.Server, error) {
	query := `SELECT id, name, join_key, cpu_type, mem_size, input_rate, output_rate, netcard,
		ip_type, ip_addr, udp_port, dns_name, server_type, ower_id, ower_callsign, status, note,
		create_time, update_time FROM servers WHERE id=?`
	row := r.db.QueryRow(query, id)

	server := &models.Server{}
	err := row.Scan(&server.ID, &server.Name, &server.JoinKey, &server.CpuType, &server.MemSize,
		&server.InputRate, &server.OutputRate, &server.NetCard, &server.IPType, &server.IPAddr,
		&server.UDPPort, &server.DNSName, &server.ServerType, &server.OwerID, &server.OwerCallSign,
		&server.Status, &server.Note, &server.CreateTime, &server.UpdateTime)
	if err != nil {
		return nil, err
	}

	return server, nil
}
