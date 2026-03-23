package gormdb

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"draarl/internal/common"

	"gorm.io/gorm"
)

// SiteConfigRepository 站点配置仓储
type SiteConfigRepository struct {
	db    *gorm.DB
	once sync.Once
}

var siteConfigInstance *SiteConfigRepository
var siteConfigOnce sync.Once

// GetSiteConfigRepo 获取站点配置仓储实例
func GetSiteConfigRepo() *SiteConfigRepository {
	siteConfigOnce.Do(func() {
		db := Get()
		siteConfigInstance = &SiteConfigRepository{db: db}
	})
	return siteConfigInstance
}

// SiteConfigValue 配置值接口
type SiteConfigValue interface{}

// ConfigCategory 配置分类
const (
	CategoryICP        = "icp"
	CategorySystem     = "system"
	CategoryAPRS       = "aprs"
	CategoryOpenAI     = "openai"
	CategoryCommConfig = "comm_config"
	CategorySMTP       = "smtp"
)

// ICPConfig ICP配置
type ICPConfig struct {
	ICP string `json:"icp"`
}

// SystemInfoConfig 系统信息配置
type SystemInfoConfig struct {
	Name          string  `json:"name"`
	NameShorthand string  `json:"nameshorthand"`
	LogoURL       string  `json:"logo_url"`
	FaviconURL    string  `json:"favicon_url"`
	Language      string  `json:"language"`
}

// APRSConfig APRS配置
type APRSConfig struct {
	APRSServerHost string  `json:"aprs_server_host"`
	APRSServerPort string  `json:"aprs_server_port"`
	SelfAddress    string  `json:"self_address"`
	SelfPort       string  `json:"self_port"`
	CallSign       string  `json:"callsign"`
	SSID           string  `json:"ssid"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	Altitude       string  `json:"altitude"`
}

// OpenAIConfig OpenAI配置
type OpenAIConfig struct {
	BaseURL string `json:"base_url"`
	APIKEY  string `json:"api_key"`
	Engine  string `json:"engine"`
}

// CommSettingsConfig 通信设置配置
type CommSettingsConfig struct {
	Enabled        bool `json:"enabled"`
	RetentionDays  int  `json:"retention_days"`
	MinDurationMs  int  `json:"min_duration_ms"`
	MaxDurationSec int  `json:"max_duration_sec"`
	BatchUploadSec int  `json:"batch_upload_sec"`
}

// SMTPConfig SMTP邮件配置
type SMTPConfig struct {
	Host        string `json:"host"`         // SMTP服务器地址
	Port        int    `json:"port"`         // SMTP端口
	UseSSL      bool   `json:"use_ssl"`      // 是否使用SSL
	SenderName  string `json:"sender_name"`  // 发件人昵称
	SenderEmail string `json:"sender_email"` // 发件人邮箱
	Password    string `json:"password"`     // 邮箱授权码
}

// GetAll 获取所有配置
func (r *SiteConfigRepository) GetAll() ([]SiteConfig, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("repository not initialized")
	}
	var configs []SiteConfig
	err := r.db.Find(&configs).Error
	return configs, err
}

// GetByCategory 根据分类获取配置
func (r *SiteConfigRepository) GetByCategory(category string) ([]SiteConfig, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("repository not initialized")
	}
	var configs []SiteConfig
	err := r.db.Where("category = ?", category).Find(&configs).Error
	return configs, err
}

// GetByKey 根据key获取配置
func (r *SiteConfigRepository) GetByKey(key string) (*SiteConfig, error) {
	var config SiteConfig
	err := r.db.Where("config_key = ?", key).First(&config).Error
	if err != nil {
		return nil, err
	}
	return &config, nil
}

// GetValue 获取配置值
func (r *SiteConfigRepository) GetValue(key string) (string, error) {
	config, err := r.GetByKey(key)
	if err != nil {
		return "", err
	}
	return config.Value, nil
}

// Set 设置配置（如果不存在则创建，存在则更新）
func (r *SiteConfigRepository) Set(key, value, category, description string) error {
	// 使用 map 来确保零值（空字符串）也能被更新
	// 注意：必须使用数据库列名，而不是结构体字段名
	updateData := map[string]interface{}{
		"config_key":   key,
		"config_value": value,
		"category":     category,
		"description":  description,
	}

	// 先尝试更新
	result := r.db.Model(&SiteConfig{}).Where("config_key = ?", key).Updates(updateData)
	if result.Error != nil {
		return result.Error
	}

	// 如果没有行被更新，说明记录不存在，创建新记录
	if result.RowsAffected == 0 {
		config := SiteConfig{
			Key:         key,
			Value:       value,
			Category:    category,
			Description: description,
		}
		return r.db.Create(&config).Error
	}

	return nil
}

// SetBatch 批量设置配置
func (r *SiteConfigRepository) SetBatch(configs []SiteConfig) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for i := range configs {
			if err := tx.Where("config_key = ?", configs[i].Key).Assign(configs[i]).FirstOrCreate(&configs[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Delete 删除配置
func (r *SiteConfigRepository) Delete(key string) error {
	return r.db.Where("config_key = ?", key).Delete(&SiteConfig{}).Error
}

// GetICPConfig 获取ICP配置
func (r *SiteConfigRepository) GetICPConfig() (*ICPConfig, error) {
	icp, err := r.GetValue("web.icp")
	if err != nil {
		// 返回默认值
		return &ICPConfig{ICP: ""}, nil
	}
	return &ICPConfig{ICP: icp}, nil
}

// SetICPConfig 设置ICP配置
func (r *SiteConfigRepository) SetICPConfig(icp string) error {
	return r.Set("web.icp", icp, CategoryICP, "网站备案号")
}

// GetSystemInfoConfig 获取系统信息配置
func (r *SiteConfigRepository) GetSystemInfoConfig() (*SystemInfoConfig, error) {
	configs, err := r.GetByCategory(CategorySystem)
	if err != nil {
		return nil, err
	}

	result := &SystemInfoConfig{
		Name:          common.SiteName,
		NameShorthand: common.SiteShortName,
		LogoURL:       "",
		FaviconURL:    "",
		Language:      "zh",
	}

	for _, config := range configs {
		switch config.Key {
		case "system.name":
			result.Name = config.Value
		case "system.nameshorthand":
			result.NameShorthand = config.Value
		case "system.logo_url":
			result.LogoURL = config.Value
		case "system.favicon_url":
			result.FaviconURL = config.Value
		case "system.language":
			result.Language = config.Value
		}
	}

	return result, nil
}

// SetSystemInfoConfig 设置系统信息配置
func (r *SiteConfigRepository) SetSystemInfoConfig(config SystemInfoConfig) error {
	configs := []SiteConfig{
		{Key: "system.name", Value: config.Name, Category: CategorySystem, Description: "站点名称"},
		{Key: "system.nameshorthand", Value: config.NameShorthand, Category: CategorySystem, Description: "站点简称"},
		{Key: "system.logo_url", Value: config.LogoURL, Category: CategorySystem, Description: "站点Logo URL"},
		{Key: "system.favicon_url", Value: config.FaviconURL, Category: CategorySystem, Description: "站点Favicon URL"},
		{Key: "system.language", Value: config.Language, Category: CategorySystem, Description: "站点语言"},
	}
	return r.SetBatch(configs)
}

// GetAPRSConfig 获取APRS配置
func (r *SiteConfigRepository) GetAPRSConfig() (*APRSConfig, error) {
	configs, err := r.GetByCategory(CategoryAPRS)
	if err != nil {
		return nil, err
	}

	result := &APRSConfig{
		APRSServerHost: "china.aprs2.net",
		APRSServerPort: "14580",
		SelfAddress:    "nrl.4l2.cn",
		SelfPort:       "60050",
		CallSign:       "",
		SSID:           "10",
		Latitude:       0,
		Longitude:      0,
		Altitude:       "000000",
	}

	for _, config := range configs {
		switch config.Key {
		case "aprs.server_host":
			result.APRSServerHost = config.Value
		case "aprs.server_port":
			result.APRSServerPort = config.Value
		case "aprs.self_address":
			result.SelfAddress = config.Value
		case "aprs.self_port":
			result.SelfPort = config.Value
		case "aprs.callsign":
			result.CallSign = config.Value
		case "aprs.ssid":
			result.SSID = config.Value
		case "aprs.latitude":
			var lat float64
			if _, err := fmt.Sscanf(config.Value, "%f", &lat); err == nil {
				result.Latitude = lat
			}
		case "aprs.longitude":
			var lon float64
			if _, err := fmt.Sscanf(config.Value, "%f", &lon); err == nil {
				result.Longitude = lon
			}
		case "aprs.altitude":
			result.Altitude = config.Value
		}
	}

	return result, nil
}

// SetAPRSConfig 设置APRS配置
func (r *SiteConfigRepository) SetAPRSConfig(config APRSConfig) error {
	configs := []SiteConfig{
		{Key: "aprs.server_host", Value: config.APRSServerHost, Category: CategoryAPRS, Description: "APRS服务器地址"},
		{Key: "aprs.server_port", Value: config.APRSServerPort, Category: CategoryAPRS, Description: "APRS服务器端口"},
		{Key: "aprs.self_address", Value: config.SelfAddress, Category: CategoryAPRS, Description: "本机地址"},
		{Key: "aprs.self_port", Value: config.SelfPort, Category: CategoryAPRS, Description: "本机端口"},
		{Key: "aprs.callsign", Value: config.CallSign, Category: CategoryAPRS, Description: "呼号"},
		{Key: "aprs.ssid", Value: config.SSID, Category: CategoryAPRS, Description: "SSID"},
		{Key: "aprs.latitude", Value: fmt.Sprintf("%.6f", config.Latitude), Category: CategoryAPRS, Description: "纬度"},
		{Key: "aprs.longitude", Value: fmt.Sprintf("%.6f", config.Longitude), Category: CategoryAPRS, Description: "经度"},
		{Key: "aprs.altitude", Value: config.Altitude, Category: CategoryAPRS, Description: "海拔高度"},
	}
	return r.SetBatch(configs)
}

// GetOpenAIConfig 获取OpenAI配置
func (r *SiteConfigRepository) GetOpenAIConfig() (*OpenAIConfig, error) {
	configs, err := r.GetByCategory(CategoryOpenAI)
	if err != nil {
		return nil, err
	}

	result := &OpenAIConfig{
		BaseURL: "",
		APIKEY:  "",
		Engine:  "",
	}

	for _, config := range configs {
		switch config.Key {
		case "openai.base_url":
			result.BaseURL = config.Value
		case "openai.api_key":
			result.APIKEY = config.Value
		case "openai.engine":
			result.Engine = config.Value
		}
	}

	return result, nil
}

// SetOpenAIConfig 设置OpenAI配置
func (r *SiteConfigRepository) SetOpenAIConfig(config OpenAIConfig) error {
	configs := []SiteConfig{
		{Key: "openai.base_url", Value: config.BaseURL, Category: CategoryOpenAI, Description: "OpenAI API Base URL"},
		{Key: "openai.api_key", Value: config.APIKEY, Category: CategoryOpenAI, Description: "OpenAI API Key"},
		{Key: "openai.engine", Value: config.Engine, Category: CategoryOpenAI, Description: "OpenAI Engine/Model"},
	}
	return r.SetBatch(configs)
}

// GetCommSettingsConfig 获取通信设置配置
func (r *SiteConfigRepository) GetCommSettingsConfig() (*CommSettingsConfig, error) {
	configs, err := r.GetByCategory(CategoryCommConfig)
	if err != nil {
		return nil, err
	}

	result := &CommSettingsConfig{
		Enabled:        false,
		RetentionDays:  30,
		MinDurationMs:  500,
		MaxDurationSec: 300,
		BatchUploadSec: 10,
	}

	for _, config := range configs {
		switch config.Key {
		case "comm.enabled":
			result.Enabled = config.Value == "true"
		case "comm.retention_days":
			if val, err := strconv.Atoi(config.Value); err == nil {
				result.RetentionDays = val
			}
		case "comm.min_duration_ms":
			if val, err := strconv.Atoi(config.Value); err == nil {
				result.MinDurationMs = val
			}
		case "comm.max_duration_sec":
			if val, err := strconv.Atoi(config.Value); err == nil {
				result.MaxDurationSec = val
			}
		case "comm.batch_upload_sec":
			if val, err := strconv.Atoi(config.Value); err == nil {
				result.BatchUploadSec = val
			}
		}
	}

	return result, nil
}

// SetCommSettingsConfig 设置通信设置配置
func (r *SiteConfigRepository) SetCommSettingsConfig(config CommSettingsConfig) error {
	enabledStr := "false"
	if config.Enabled {
		enabledStr = "true"
	}
	configs := []SiteConfig{
		{Key: "comm.enabled", Value: enabledStr, Category: CategoryCommConfig, Description: "是否启用通信记录"},
		{Key: "comm.retention_days", Value: strconv.Itoa(config.RetentionDays), Category: CategoryCommConfig, Description: "数据保留天数"},
		{Key: "comm.min_duration_ms", Value: strconv.Itoa(config.MinDurationMs), Category: CategoryCommConfig, Description: "最小录制阈值(毫秒)"},
		{Key: "comm.max_duration_sec", Value: strconv.Itoa(config.MaxDurationSec), Category: CategoryCommConfig, Description: "最大录制时长(秒)"},
		{Key: "comm.batch_upload_sec", Value: strconv.Itoa(config.BatchUploadSec), Category: CategoryCommConfig, Description: "批量上传间隔(秒)"},
	}
	return r.SetBatch(configs)
}

// GetAllConfigMap 获取所有配置的map形式
func (r *SiteConfigRepository) GetAllConfigMap() (map[string]string, error) {
	configs, err := r.GetAll()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, config := range configs {
		result[config.Key] = config.Value
	}
	return result, nil
}

// InitDefaultConfigs 初始化默认配置（从YAML配置迁移）
func (r *SiteConfigRepository) InitDefaultConfigs(yamlICP, yamlName, yamlNameShorthand, yamlLogoURL, yamlLanguage string,
	yamlAPRSServerHost, yamlAPRSServerPort, yamlSelfAddress, yamlSelfPort, yamlCallSign, yamlSSID, yamlAltitude string,
	yamlLatitude, yamlLongitude float64,
	yamlOpenAIBaseURL, yamlOpenAIAPIKey, yamlOpenAIEngine string) error {

	// ICP配置
	if err := r.SetICPConfig(yamlICP); err != nil {
		return err
	}

	// 系统信息配置
	systemConfig := SystemInfoConfig{
		Name:          yamlName,
		NameShorthand: yamlNameShorthand,
		LogoURL:       yamlLogoURL,
		Language:      yamlLanguage,
	}
	if err := r.SetSystemInfoConfig(systemConfig); err != nil {
		return err
	}

	// APRS配置
	aprsConfig := APRSConfig{
		APRSServerHost: yamlAPRSServerHost,
		APRSServerPort: yamlAPRSServerPort,
		SelfAddress:    yamlSelfAddress,
		SelfPort:       yamlSelfPort,
		CallSign:       yamlCallSign,
		SSID:           yamlSSID,
		Latitude:       yamlLatitude,
		Longitude:      yamlLongitude,
		Altitude:       yamlAltitude,
	}
	if err := r.SetAPRSConfig(aprsConfig); err != nil {
		return err
	}

	// OpenAI配置
	openaiConfig := OpenAIConfig{
		BaseURL: yamlOpenAIBaseURL,
		APIKEY:  yamlOpenAIAPIKey,
		Engine:  yamlOpenAIEngine,
	}
	if err := r.SetOpenAIConfig(openaiConfig); err != nil {
		return err
	}

	return nil
}

// ToJSON 辅助函数：将对象转为JSON字符串
func ToJSON(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FromJSON 辅助函数：从JSON字符串解析到对象
func FromJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}

// GetSMTPConfig 获取SMTP配置
func (r *SiteConfigRepository) GetSMTPConfig() (*SMTPConfig, error) {
	configs, err := r.GetByCategory(CategorySMTP)
	if err != nil {
		return nil, err
	}

	result := &SMTPConfig{
		Host:        "smtp.qq.com",
		Port:        465,
		UseSSL:      true,
		SenderName:  "DraARL麟链",
		SenderEmail: "",
		Password:    "",
	}

	for _, config := range configs {
		switch config.Key {
		case "smtp.host":
			result.Host = config.Value
		case "smtp.port":
			if val, err := strconv.Atoi(config.Value); err == nil {
				result.Port = val
			}
		case "smtp.use_ssl":
			result.UseSSL = config.Value == "true"
		case "smtp.sender_name":
			result.SenderName = config.Value
		case "smtp.sender_email":
			result.SenderEmail = config.Value
		case "smtp.password":
			result.Password = config.Value
		}
	}

	return result, nil
}

// SetSMTPConfig 设置SMTP配置
func (r *SiteConfigRepository) SetSMTPConfig(config SMTPConfig) error {
	useSSLStr := "false"
	if config.UseSSL {
		useSSLStr = "true"
	}
	configs := []SiteConfig{
		{Key: "smtp.host", Value: config.Host, Category: CategorySMTP, Description: "SMTP服务器地址"},
		{Key: "smtp.port", Value: strconv.Itoa(config.Port), Category: CategorySMTP, Description: "SMTP端口"},
		{Key: "smtp.use_ssl", Value: useSSLStr, Category: CategorySMTP, Description: "是否使用SSL"},
		{Key: "smtp.sender_name", Value: config.SenderName, Category: CategorySMTP, Description: "发件人昵称"},
		{Key: "smtp.sender_email", Value: config.SenderEmail, Category: CategorySMTP, Description: "发件人邮箱"},
		{Key: "smtp.password", Value: config.Password, Category: CategorySMTP, Description: "邮箱授权码"},
	}
	return r.SetBatch(configs)
}
