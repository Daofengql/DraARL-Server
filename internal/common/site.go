package common

import "draarl/internal/buildinfo"

// 站点默认配置常量
// 构建时统一的默认值，运行时可被数据库配置覆盖
const (
	// SiteName 站点名称
	SiteName = "麟链互联"
	// SiteShortName 站点短名称/简称
	SiteShortName = "DraARL"
	// ProtocolVersion 协议版本
	ProtocolVersion = "DraARLv1"
)

// SystemVersion 系统版本，由构建脚本统一注入。
var SystemVersion = buildinfo.VersionString()
