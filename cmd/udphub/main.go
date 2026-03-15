package main

import (
	"flag"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"nrllink/internal/aprs"
	"nrllink/internal/config"
	"nrllink/internal/db"
	gormdb "nrllink/internal/gormdb"
	oplog "nrllink/internal/log"
	"nrllink/internal/server"
	"nrllink/internal/udphub"
	"nrllink/pkg/cache"
	"nrllink/pkg/geoip"
	"nrllink/pkg/redis"
)

var (
	version   = "1.0.0"
	buildTime = "unknown"
	// release 模式标志，通过 ldflags 设置
	// release 模式下会禁用 gin 和 gorm 的调试日志
	isRelease = "false"
)

// 命令行参数
var (
	autoMigrate = flag.Bool("auto-migrate", false, "强制执行数据库自动迁移")
)

func main() {
	// release 模式下禁用 gin 调试日志
	if isRelease == "true" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 解析命令行参数
	configPath := flag.String("c", "", "配置文件路径")
	showVersion := flag.Bool("v", false, "显示版本信息")
	printConfig := flag.String("p", "", "打印配置信息")
	resetAdminPass := flag.String("reset-admin-pass", "", "重置管理员密码（需要提供新密码）")
	flag.Parse()

	if *showVersion {
		fmt.Printf("nrllink version %s (build time: %s)\n", version, buildTime)
		os.Exit(0)
	}

	// 如果只是重置密码，不需要启动服务
	if *resetAdminPass != "" {
		resetAdminPassword(*resetAdminPass, *configPath)
		os.Exit(0)
	}

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		stdlog.Fatalf("加载配置文件失败: %v", err)
	}

	// 打印配置信息
	if *printConfig != "" {
		switch *printConfig {
		case "json":
			fmt.Printf("配置: UDP端口=%s, Web端口=%s, MySQL=%s@%s:%d/%s\n",
				cfg.System.Port, cfg.Web.Port, cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName)
		default:
			fmt.Printf("配置: UDP端口=%s, Web端口=%s, MySQL=%s@%s:%d/%s\n",
				cfg.System.Port, cfg.Web.Port, cfg.Database.User, cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName)
		}
		os.Exit(0)
	}

	// 初始化 MySQL 数据库（原生 SQL - 保持兼容）
	dsn := cfg.GetDSN()
	err = db.Init(dsn, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.Database.MaxLifetime)
	if err != nil {
		stdlog.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.Close()

	// 初始化 GORM 数据库
	gormLogLevel := "info"
	if isRelease == "true" {
		gormLogLevel = "error" // release 模式下只记录错误
	}
	gormCfg := &gormdb.Config{
		DSN:          dsn,
		MaxOpenConns: cfg.Database.MaxOpenConns,
		MaxIdleConns: cfg.Database.MaxIdleConns,
		MaxLifetime:  cfg.Database.MaxLifetime,
		LogLevel:     gormLogLevel,
	}
	if err := gormdb.Init(gormCfg); err != nil {
		stdlog.Fatalf("初始化 GORM 数据库失败: %v", err)
	}
	defer gormdb.Close()

	// 只有在指定 -auto-migrate 参数时才执行数据库迁移
	if *autoMigrate {
		stdlog.Println("执行数据库自动迁移...")

		// 自动迁移表结构（创建新表或更新表结构）
		if err := gormdb.AutoMigrate(); err != nil {
			stdlog.Fatalf("数据库表迁移失败: %v", err)
		}
		stdlog.Println("GORM 表结构迁移完成")

		// 更新数据库结构（添加缺失的列）
		if err := db.UpdateDatabase(); err != nil {
			stdlog.Fatalf("数据库结构更新失败: %v", err)
		}
		stdlog.Println("数据库结构更新完成")
	}

	// 初始化管理员用户（首次启动时）
	adminUser, adminPass, err := db.InitAdminUser()
	if err != nil {
		stdlog.Printf("初始化管理员用户失败: %v", err)
	} else if adminUser != "" {
		stdlog.Println("===========================================")
		stdlog.Println("首次启动，已创建默认管理员用户：")
		stdlog.Printf("  用户名: %s", adminUser)
		stdlog.Printf("  密码: %s", adminPass)
		stdlog.Println("  请登录后立即修改密码！")
		stdlog.Println("===========================================")
	}

	// 启动操作日志处理器
	oplog.Start()
	oplog.AddLog("系统启动", "system", 0, "", "", "")

	// 初始化 Redis（必需服务）
	if err := redis.Init(cfg); err != nil {
		stdlog.Fatalf("初始化 Redis 失败: %v", err)
	}
	defer redis.Close()
	stdlog.Println("Redis 初始化成功")

	// 初始化缓存管理器（依赖 Redis）
	if err := cache.InitManager(); err != nil {
		stdlog.Fatalf("初始化缓存管理器失败: %v", err)
	}
	stdlog.Println("缓存管理器初始化成功")

	// 初始化 IP 地理位置数据库
	if cfg.System.IPFile != "" {
		if err := geoip.Init(cfg.System.IPFile); err != nil {
			stdlog.Printf("IP 地理位置数据库初始化失败: %v", err)
		} else {
			stdlog.Println("IP 地理位置数据库初始化成功")
		}
	}

	// 获取 UDP 端口号
	udpPort := 8000
	if cfg.System.Port != "" {
		fmt.Sscanf(cfg.System.Port, "%d", &udpPort)
	}

	// 启动 UDP 服务器（核心语音转发服务）
	go func() {
		stdlog.Println("正在启动 UDP 服务器...")
		if err := udphub.StartUDPServer(udpPort); err != nil {
			stdlog.Printf("UDP 服务器启动失败: %v", err)
		}
	}()

	// 启动 APRS 服务（配置从数据库加载）
	stdlog.Println("正在启动 APRS 服务...")
	aprs.StartAPRSService()

	// 启动 HTTP 服务器（Web API 和前端服务）
	go func() {
		stdlog.Println("正在启动 HTTP 服务器...")
		srv := server.New(cfg)
		if err := srv.Start(); err != nil {
			stdlog.Printf("HTTP 服务器错误: %v", err)
		}
	}()

	stdlog.Printf("nrllink v%s 启动成功", version)
	stdlog.Printf("配置: UDP端口=%s, Web端口=%s, MySQL=%s:%d/%s",
		cfg.System.Port, cfg.Web.Port, cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName)

	// 等待信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	stdlog.Println("正在关闭服务...")

	// 停止 APRS 服务
	aprs.StopAPRSService()

	// 刷新日志缓冲区
	oplog.Flush()

	stdlog.Println("nrllink 已关闭")
}

// resetAdminPassword 重置管理员密码
func resetAdminPassword(newPassword, configPath string) {
	// 加载配置以获取数据库连接信息
	cfg, err := config.Load(configPath)
	if err != nil {
		stdlog.Fatalf("加载配置文件失败: %v", err)
	}

	// 初始化数据库
	dsn := cfg.GetDSN()
	err = db.Init(dsn, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.Database.MaxLifetime)
	if err != nil {
		stdlog.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.Close()

	// 使用 GORM 重置密码
	gormCfg := &gormdb.Config{
		DSN:          dsn,
		MaxOpenConns: cfg.Database.MaxOpenConns,
		MaxIdleConns: cfg.Database.MaxIdleConns,
		MaxLifetime:  cfg.Database.MaxLifetime,
		LogLevel:     "silent",
	}
	if err := gormdb.Init(gormCfg); err != nil {
		stdlog.Fatalf("初始化 GORM 数据库失败: %v", err)
	}
	defer gormdb.Close()

	// 获取用户仓库
	userRepo := gormdb.NewUserRepository()

	// 查找管理员用户
	users, _, err := userRepo.ListUsers(100, 1)
	if err != nil {
		stdlog.Fatalf("查询用户失败: %v", err)
	}

	var adminUser *gormdb.User
	for i := range users {
		// 检查是否是管理员（roles 包含 "admin"）
		if users[i].Roles == `["admin"]` || users[i].Roles == `[admin]` || users[i].Roles == "admin" {
			adminUser = users[i]
			break
		}
	}

	if adminUser == nil {
		stdlog.Fatal("未找到管理员用户")
	}

	// 更新密码（使用 bcrypt）
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		stdlog.Fatalf("密码加密失败: %v", err)
	}

	adminUser.Password = string(hashedPassword)
	if err := userRepo.UpdateUserPassword(adminUser.ID, adminUser.Password); err != nil {
		stdlog.Fatalf("更新密码失败: %v", err)
	}

	fmt.Println("========================================")
	fmt.Printf("管理员密码已重置成功！\n")
	fmt.Printf("用户名: %s\n", adminUser.Name)
	fmt.Printf("新密码: %s\n", newPassword)
	fmt.Println("========================================")
}
