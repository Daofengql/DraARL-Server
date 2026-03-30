package udphub

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"time"

	"draarl/internal/gormdb"
	"draarl/internal/models"
	"draarl/internal/protocol"
)

// ==========================================
// Config 包协议常量 (Type=0x03)
// 仅用于 UDP 普通设备的配置同步
// ==========================================

// Config 包 DATA 区域操作类型
const (
	ConfigTypeQuery     byte = 0x01 // 查询配置请求
	ConfigTypeSet       byte = 0x02 // 配置下发/上报
	ConfigTypeTimeSync  byte = 0x03 // 时间同步
)

// TLV 配置项 Type 定义
const (
	TLVTypeRxFreq     byte = 0x01 // 接收频率 (8 bytes, big-endian uint64 Hz)
	TLVTypeTxFreq     byte = 0x02 // 发射频率 (8 bytes, big-endian uint64 Hz)
	TLVTypeRxCtcss    byte = 0x03 // 接收亚音 (4 bytes, big-endian float32 Hz, 0=关闭)
	TLVTypeTxCtcss    byte = 0x04 // 发射亚音 (4 bytes, big-endian float32 Hz, 0=关闭)
	TLVTypeSqlLevel   byte = 0x05 // 静噪等级 (1 byte, uint8 0-9)
	TLVTypePowerLevel byte = 0x06 // 功率等级 (1 byte, uint8 1=低, 2=中, 3=高)
	TLVTypeTxBandwidth byte = 0x07 // 发射带宽 (1 byte, uint8 1=窄带, 2=宽带)
	TLVTypeTimestamp  byte = 0x10 // 时间戳 (8 bytes, big-endian int64 Unix毫秒)
)

// 配置键名映射 (TLV Type -> 数据库 Key)
var tlvTypeToKeyMap = map[byte]string{
	TLVTypeRxFreq:     "rx_freq",
	TLVTypeTxFreq:     "tx_freq",
	TLVTypeRxCtcss:    "rx_ctcss",
	TLVTypeTxCtcss:    "tx_ctcss",
	TLVTypeSqlLevel:   "sql_level",
	TLVTypePowerLevel: "power_level",
	TLVTypeTxBandwidth: "tx_bandwidth",
	TLVTypeTimestamp:  "timestamp",
}

// 配置键名反向映射 (数据库 Key -> TLV Type)
var keyToTlvTypeMap = map[string]byte{
	"rx_freq":     TLVTypeRxFreq,
	"tx_freq":     TLVTypeTxFreq,
	"rx_ctcss":    TLVTypeRxCtcss,
	"tx_ctcss":    TLVTypeTxCtcss,
	"sql_level":   TLVTypeSqlLevel,
	"power_level": TLVTypePowerLevel,
	"tx_bandwidth": TLVTypeTxBandwidth,
	"timestamp":   TLVTypeTimestamp,
}

// TLV 长度定义
var tlvLengthMap = map[byte]int{
	TLVTypeRxFreq:     8,
	TLVTypeTxFreq:     8,
	TLVTypeRxCtcss:    4,
	TLVTypeTxCtcss:    4,
	TLVTypeSqlLevel:   1,
	TLVTypePowerLevel: 1,
	TLVTypeTxBandwidth: 1,
	TLVTypeTimestamp:  8,
}

// DeviceConfig 设备配置结构体（用于内存表示）
type DeviceConfig struct {
	RxFreq     uint64  // 接收频率 (Hz)
	TxFreq     uint64  // 发射频率 (Hz)
	RxCtcss    float32 // 接收亚音 (Hz, 0=关闭)
	TxCtcss    float32 // 发射亚音 (Hz, 0=关闭)
	SqlLevel   uint8   // 静噪等级 (0-9)
	PowerLevel uint8   // 功率等级 (1=低, 2=中, 3=高)
	TxBandwidth uint8  // 发射带宽 (1=窄带, 2=宽带)
}

// ==========================================
// TLV 编解码函数
// ==========================================

// encodeTLV 将配置 map 编码为 TLV 格式的 []byte
// 返回: 完整的 TLV 列表（不含 DATA[0] 和 DATA[1]）
func encodeTLV(configs map[string]string) []byte {
	if len(configs) == 0 {
		return nil
	}

	// 预估容量：最多 7 个配置项，每个最大 10 字节 (1+1+8)
	result := make([]byte, 0, len(configs)*10)

	for key, value := range configs {
		tlvType, ok := keyToTlvTypeMap[key]
		if !ok {
			continue // 忽略未知的配置键
		}

		length, ok := tlvLengthMap[tlvType]
		if !ok {
			continue
		}

		// 编码 TLV
		result = append(result, tlvType) // Type (1 byte)
		result = append(result, byte(length)) // Length (1 byte)

		// Value (N bytes)
		valueBytes := encodeTLVValue(tlvType, value)
		result = append(result, valueBytes...)
	}

	return result
}

// encodeTLVValue 编码单个 TLV 值
func encodeTLVValue(tlvType byte, value string) []byte {
	switch tlvType {
	case TLVTypeRxFreq, TLVTypeTxFreq:
		// 8 bytes, big-endian uint64
		var freq uint64
		if _, err := fmt.Sscanf(value, "%d", &freq); err != nil {
			freq = 0
		}
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, freq)
		return buf

	case TLVTypeRxCtcss, TLVTypeTxCtcss:
		// 4 bytes, big-endian float32
		var ctcss float64
		if _, err := fmt.Sscanf(value, "%f", &ctcss); err != nil {
			ctcss = 0
		}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, math.Float32bits(float32(ctcss)))
		return buf

	case TLVTypeSqlLevel, TLVTypePowerLevel, TLVTypeTxBandwidth:
		// 1 byte, uint8
		var val uint8
		if _, err := fmt.Sscanf(value, "%d", &val); err != nil {
			val = 0
		}
		return []byte{val}

	case TLVTypeTimestamp:
		// 8 bytes, big-endian int64 (Unix毫秒)
		var ts int64
		if _, err := fmt.Sscanf(value, "%d", &ts); err != nil {
			ts = time.Now().UnixMilli()
		}
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(ts))
		return buf

	default:
		return make([]byte, tlvLengthMap[tlvType])
	}
}

// decodeTLV 将 TLV 格式的 []byte 解码为配置 map
// 输入: DATA[2:] 开始的 TLV 列表（跳过 DATA[0] 和 DATA[1]）
func decodeTLV(data []byte) map[string]string {
	result := make(map[string]string)

	if len(data) == 0 {
		return result
	}

	offset := 0
	for offset < len(data) {
		if offset+2 > len(data) {
			break // 剩余字节不足以解析 TLV 头
		}

		tlvType := data[offset]
		length := int(data[offset+1])
		offset += 2

		if offset+length > len(data) {
			break // 剩余字节不足以解析 Value
		}

		valueBytes := data[offset : offset+length]
		offset += length

		key, ok := tlvTypeToKeyMap[tlvType]
		if !ok {
			continue // 忽略未知的 TLV Type
		}

		value := decodeTLVValue(tlvType, valueBytes)
		result[key] = value
	}

	return result
}

// decodeTLVValue 解码单个 TLV 值
func decodeTLVValue(tlvType byte, data []byte) string {
	switch tlvType {
	case TLVTypeRxFreq, TLVTypeTxFreq:
		// 8 bytes, big-endian uint64
		if len(data) < 8 {
			return "0"
		}
		freq := binary.BigEndian.Uint64(data)
		return fmt.Sprintf("%d", freq)

	case TLVTypeRxCtcss, TLVTypeTxCtcss:
		// 4 bytes, big-endian float32
		if len(data) < 4 {
			return "0"
		}
		ctcss := math.Float32frombits(binary.BigEndian.Uint32(data))
		return fmt.Sprintf("%.1f", ctcss)

	case TLVTypeSqlLevel, TLVTypePowerLevel, TLVTypeTxBandwidth:
		// 1 byte, uint8
		if len(data) < 1 {
			return "0"
		}
		return fmt.Sprintf("%d", data[0])

	case TLVTypeTimestamp:
		// 8 bytes, big-endian int64 (Unix毫秒)
		if len(data) < 8 {
			return "0"
		}
		ts := int64(binary.BigEndian.Uint64(data))
		return fmt.Sprintf("%d", ts)

	default:
		return ""
	}
}

// ==========================================
// Config 包构建函数
// ==========================================

// buildConfigQueryPacket 构建配置查询包 (DATA[0] = 0x01)
func buildConfigQueryPacket() []byte {
	return []byte{ConfigTypeQuery}
}

// buildConfigSetPacket 构建配置下发/上报包 (DATA[0] = 0x02)
// configs: 要下发的配置项 map
func buildConfigSetPacket(configs map[string]string) []byte {
	if len(configs) == 0 {
		return []byte{ConfigTypeSet, 0x00}
	}

	tlvData := encodeTLV(configs)
	result := make([]byte, 2+len(tlvData))
	result[0] = ConfigTypeSet
	result[1] = byte(len(configs)) // 配置项数量
	copy(result[2:], tlvData)

	return result
}

// buildTimeSyncPacket 构建时间同步包 (DATA[0] = 0x03)
func buildTimeSyncPacket() []byte {
	result := make([]byte, 10)
	result[0] = ConfigTypeTimeSync
	timestamp := time.Now().UnixMilli()
	binary.BigEndian.PutUint64(result[2:10], uint64(timestamp))
	return result
}

// ==========================================
// 配置同步核心函数
// ==========================================

// sendConfigToDevice 向设备下发配置
// 仅下发指定的配置项（动态下发）
func sendConfigToDevice(dev *models.Device, configs map[string]string) error {
	if dev == nil || dev.UDPAddr == nil || globalConn == nil {
		return fmt.Errorf("device not ready")
	}

	if len(configs) == 0 {
		return nil // 无配置需要下发
	}

	// 构建 Config 包
	data := buildConfigSetPacket(configs)
	packet := protocol.EncodeDraARLv1(
		dev.Username,
		"", // 密码为空
		dev.SSID,
		protocol.DraARLTypeConfig,
		0, // DevModel
		0, // DMRID
		dev.CallSign,
		data,
	)

	// 发送到设备
	_, err := globalConn.WriteToUDP(packet, dev.UDPAddr)
	if err != nil {
		return fmt.Errorf("send config failed: %w", err)
	}

	log.Printf("[CONFIG] 发送配置到设备 %s-%d: %d 项", dev.CallSign, dev.SSID, len(configs))
	return nil
}

// queryDeviceConfig 向设备发送配置查询请求
func queryDeviceConfig(dev *models.Device) error {
	if dev == nil || dev.UDPAddr == nil || globalConn == nil {
		return fmt.Errorf("device not ready")
	}

	// 构建查询包
	data := buildConfigQueryPacket()
	packet := protocol.EncodeDraARLv1(
		dev.Username,
		"", // 密码为空
		dev.SSID,
		protocol.DraARLTypeConfig,
		0, // DevModel
		0, // DMRID
		dev.CallSign,
		data,
	)

	// 发送到设备
	_, err := globalConn.WriteToUDP(packet, dev.UDPAddr)
	if err != nil {
		return fmt.Errorf("send query failed: %w", err)
	}

	log.Printf("[CONFIG] 发送配置查询到设备 %s-%d", dev.CallSign, dev.SSID)
	return nil
}

// sendTimeSync 向设备发送时间同步
func sendTimeSync(dev *models.Device) error {
	if dev == nil || dev.UDPAddr == nil || globalConn == nil {
		return fmt.Errorf("device not ready")
	}

	// 构建时间同步包
	data := buildTimeSyncPacket()
	packet := protocol.EncodeDraARLv1(
		dev.Username,
		"", // 密码为空
		dev.SSID,
		protocol.DraARLTypeConfig,
		0, // DevModel
		0, // DMRID
		dev.CallSign,
		data,
	)

	// 发送到设备
	_, err := globalConn.WriteToUDP(packet, dev.UDPAddr)
	if err != nil {
		return fmt.Errorf("send time sync failed: %w", err)
	}

	log.Printf("[CONFIG] 发送时间同步到设备 %s-%d", dev.CallSign, dev.SSID)
	return nil
}

// SyncDeviceConfig 设备上线时同步配置
// 如果数据库中有配置记录，则下发配置；否则发送查询请求
// 无论哪种情况，最后都会发送时间同步包
func SyncDeviceConfig(dev *models.Device) {
	if dev == nil {
		return
	}

	repo := gormdb.NewDeviceConfigRepository()
	hasConfigs, err := repo.HasDeviceConfigs(dev.ID)
	if err != nil {
		log.Printf("[CONFIG] 查询设备配置失败: %v", err)
		return
	}

	if hasConfigs {
		// 数据库中有配置记录，下发配置到设备
		configs, err := repo.GetDeviceConfigs(dev.ID)
		if err != nil {
			log.Printf("[CONFIG] 获取设备配置失败: %v", err)
			return
		}

		// 过滤掉 timestamp，只下发设备参数
		paramConfigs := make(map[string]string)
		for k, v := range configs {
			if k != "timestamp" {
				paramConfigs[k] = v
			}
		}

		if len(paramConfigs) > 0 {
			if err := sendConfigToDevice(dev, paramConfigs); err != nil {
				log.Printf("[CONFIG] 下发配置失败: %v", err)
			}
		}
	} else {
		// 数据库中没有配置记录，发送查询请求
		if err := queryDeviceConfig(dev); err != nil {
			log.Printf("[CONFIG] 发送查询请求失败: %v", err)
		}
	}

	// 无论是否有配置，都发送时间同步包
	if err := sendTimeSync(dev); err != nil {
		log.Printf("[CONFIG] 发送时间同步失败: %v", err)
	}
}

// HandleDeviceConfigReport 处理设备上报的配置
// 解析 TLV 数据，与数据库比对，存储变化的配置
// 处理完成后发送时间同步包作为 ACK
func HandleDeviceConfigReport(dev *models.Device, data []byte) {
	if dev == nil || len(data) < 2 {
		return
	}

	// 检查是否为配置上报包 (DATA[0] = 0x02)
	if data[0] != ConfigTypeSet {
		return
	}

	// 解析 TLV 数据
	configs := decodeTLV(data[2:])
	if len(configs) == 0 {
		return
	}

	// 存储到数据库（仅更新变化的配置）
	repo := gormdb.NewDeviceConfigRepository()
	updatedCount, err := repo.UpdateDeviceConfigsIfChanged(dev.ID, configs)
	if err != nil {
		log.Printf("[CONFIG] 保存设备配置失败: %v", err)
		return
	}

	if updatedCount > 0 {
		log.Printf("[CONFIG] 设备 %s-%d 上报配置，更新了 %d 项", dev.CallSign, dev.SSID, updatedCount)
	}

	// 发送时间同步包作为 ACK（同时同步时间）
	if err := sendTimeSync(dev); err != nil {
		log.Printf("[CONFIG] 发送时间同步ACK失败: %v", err)
	}
}

// SendConfigToDeviceByID 通过设备ID发送配置（供 API 调用）
func SendConfigToDeviceByID(deviceID int, configs map[string]string) error {
	// 从内存查找设备
	dev := GetDeviceByID(deviceID)
	if dev == nil {
		return fmt.Errorf("device not found")
	}

	if !dev.ISOnline || dev.UDPAddr == nil {
		return fmt.Errorf("device is offline")
	}

	return sendConfigToDevice(dev, configs)
}

// GetDeviceConfigsFromDB 从数据库获取设备配置（供 API 调用）
func GetDeviceConfigsFromDB(deviceID int) (map[string]string, error) {
	repo := gormdb.NewDeviceConfigRepository()
	return repo.GetDeviceConfigs(deviceID)
}

// SaveDeviceConfigsToDB 保存设备配置到数据库（供 API 调用）
// 同时如果设备在线，则下发配置
func SaveDeviceConfigsToDB(deviceID int, configs map[string]string) error {
	repo := gormdb.NewDeviceConfigRepository()

	// 保存到数据库
	if err := repo.SetDeviceConfigs(deviceID, configs); err != nil {
		return err
	}

	// 如果设备在线，下发配置
	dev := GetDeviceByID(deviceID)
	if dev != nil && dev.ISOnline && dev.UDPAddr != nil {
		if err := sendConfigToDevice(dev, configs); err != nil {
			log.Printf("[CONFIG] 下发配置到在线设备失败: %v", err)
			// 不返回错误，因为数据库已保存成功
		}
	}

	return nil
}
