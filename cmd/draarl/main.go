package main

import (
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"draarl/internal/aprs"
	authstore "draarl/internal/auth"
	"draarl/internal/buildinfo"
	"draarl/internal/config"
	"draarl/internal/db"
	"draarl/internal/ghostsession"
	gormdb "draarl/internal/gormdb"
	"draarl/internal/interconnect"
	oplog "draarl/internal/log"
	"draarl/internal/middleware"
	"draarl/internal/server"
	"draarl/internal/udphub"
	"draarl/pkg/cache"
	"draarl/pkg/crypto"
	"draarl/pkg/geoip"
	"draarl/pkg/jwt"
	"draarl/pkg/storage"
)

// 命令行参数
var (
	autoMigrate = flag.Bool("auto-migrate", false, "强制执行数据库自动迁移")
)

func main() {
	config.SetReleaseBuild(buildinfo.IsRelease())

	// release 模式下禁用 gin 调试日志
	if buildinfo.IsRelease() {
		gin.SetMode(gin.ReleaseMode)
	}

	// 解析命令行参数
	configPath := flag.String("c", "", "配置文件路径")
	edgeMode := flag.Bool("edge", false, "以无数据库边缘节点模式启动")
	interconnectMode := flag.Bool("interconnect", false, "以无数据库边缘节点模式启动（edge 别名）")
	showVersion := flag.Bool("v", false, "显示版本信息")
	printConfig := flag.String("p", "", "打印配置信息")
	resetAdminPass := flag.String("reset-admin-pass", "", "重置管理员密码（需要提供新密码）")
	migrateStorage := flag.String("migrate-storage", "", "迁移存储驱动或 profile，格式 from:to（如 minio:local 或 minio-prod:r2-prod），不启动主服务")
	migrateDelete := flag.Bool("migrate-delete-source", false, "迁移成功并校验后删除源端对象（默认保留源端）")
	migrateDryRun := flag.Bool("migrate-dry-run", false, "仅统计迁移计划，不实际写入目标端")
	flag.Parse()
	if *edgeMode || *interconnectMode {
		if err := runEdgeMode(*configPath); err != nil {
			stdlog.Fatalf("边缘节点启动失败: %v", err)
		}
		return
	}

	if *showVersion {
		fmt.Printf("DraARL version %s (build time: %s)\n", buildinfo.VersionString(), buildinfo.BuildTimeString())
		os.Exit(0)
	}

	// 如果只是重置密码，不需要启动服务
	if *resetAdminPass != "" {
		resetAdminPassword(*resetAdminPass, *configPath)
		os.Exit(0)
	}

	// 存储迁移：纯命令行，不启动主服务
	if *migrateStorage != "" {
		migrateStorageEngine(*migrateStorage, *configPath, *migrateDelete, *migrateDryRun)
		os.Exit(0)
	}

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		stdlog.Fatalf("加载配置文件失败: %v", err)
	}
	ghostsession.ConfigureGlobal(
		cfg.GhostSessions.MaxSessionsPerOwner,
		cfg.GhostSessions.MaxSubscriptionsPerSession,
		ghostSessionPolicy(cfg.GhostSessions),
	)

	// 初始化 JWT 密钥
	if err := initJWTSecret(cfg); err != nil {
		stdlog.Fatalf("初始化JWT密钥失败: %v", err)
	}

	if err := cfg.ValidateAllowedOrigins(); err != nil {
		stdlog.Fatalf("Web Origin 配置无效: %v", err)
	}
	if err := authstore.InitRefreshTokenStore(cfg); err != nil {
		stdlog.Fatalf("初始化 refresh token 存储失败: %v", err)
	}
	defer authstore.CloseRefreshTokenStore()

	// 初始化 AES 加密器（用于设备密码加密）
	if err := crypto.InitAES(cfg.DeviceAuth.AESKey); err != nil {
		stdlog.Fatalf("初始化AES加密器失败: %v", err)
	}
	stdlog.Println("AES 加密器初始化成功")

	// 初始化设备接口限速器
	middleware.InitDeviceRateLimiter()
	stdlog.Println("设备接口限速器初始化成功")

	// 初始化待绑定设备管理器
	udphub.InitPendingDeviceManager()
	stdlog.Println("待绑定设备管理器初始化成功")

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
	if buildinfo.IsRelease() {
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

	// 全新空库可安全地自动初始化；任何非空库仍要求显式参数，避免
	// 启动过程静默修改已有或半初始化结构。
	runMigration := *autoMigrate
	if !runMigration {
		empty, checkErr := gormdb.IsSchemaEmpty()
		if checkErr != nil {
			stdlog.Fatalf("检查数据库结构失败: %v", checkErr)
		}
		if empty {
			runMigration = true
			stdlog.Println("检测到目标数据库为空，将自动初始化表结构")
		}
	}
	if runMigration {
		stdlog.Println("执行数据库自动迁移...")

		// 自动迁移表结构（创建新表或更新表结构）
		// 包含数据清洗和外键约束建立的完整迁移逻辑
		if err := gormdb.AutoMigrate(); err != nil {
			stdlog.Fatalf("数据库表迁移失败: %v", err)
		}
		stdlog.Println("数据库迁移完成（含外键约束）")
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

	// 初始化缓存管理器
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
	udpPort := 60050
	if cfg.System.Port != "" {
		fmt.Sscanf(cfg.System.Port, "%d", &udpPort)
	}

	// 先等待共享 UDP socket 和 udphub pipeline 就绪。Type 0 与普通设备
	// 共用这个端口，TLS 节点控制面只能在此后启动。
	udpReady := make(chan error, 1)
	udpErrCh := make(chan error, 1)
	go func() {
		stdlog.Println("正在启动 UDP 服务器...")
		udpErrCh <- udphub.StartUDPServerWithReady(udpPort, udpReady)
	}()
	if err := <-udpReady; err != nil {
		stdlog.Fatalf("UDP 服务器启动失败: %v", err)
	}

	var centerRuntime *interconnect.CenterRuntime
	if cfg.Interconnect.Enabled {
		centerRuntime, err = startCenterInterconnect(cfg)
		if err != nil {
			stdlog.Fatalf("启动中心节点互联服务失败: %v", err)
		}
		defer centerRuntime.Close()
		interconnect.SetActiveCenterRuntime(centerRuntime)
		defer interconnect.SetActiveCenterRuntime(nil)
		centerRuntime.Cluster.SetLocalDelivery(func(frame interconnect.RelayFrame) {
			udphub.DeliverInterconnectPacket(frame.DomainID, frame.InnerPacket)
		})
		centerRuntime.Gateway.SetLocalRevocationHandler(func(deviceID, ownerID int, ssid byte, sessionID, sessionEpoch uint64) {
			udphub.RevokeCenterLocalSession(deviceID, ownerID, ssid, sessionID, sessionEpoch)
		})
		centerRuntime.Gateway.SetDeviceRevocationHandler(func(nodeID string, controlSessionID uint64, deviceID int, reason string) {
			cleared, clearErr := gormdb.NewDeviceRepository().ClearDeviceEntryIfSession(deviceID, nodeID, controlSessionID)
			if clearErr != nil {
				stdlog.Printf("clear revoked device entry failed: device=%d node=%s reason=%s err=%v", deviceID, nodeID, reason, clearErr)
				return
			}
			if cleared {
				udphub.ClearRuntimeDeviceEntryIfSession(deviceID, nodeID, controlSessionID)
			}
		})
		udphub.SetCenterInterconnectHooks(udphub.CenterInterconnectHooks{
			Activate: func(source *udphub.CenterLocalSource) error {
				grant := localSourceGrant(source)
				if err := centerRuntime.Gateway.ActivateLocalDevice(&grant); err != nil {
					return err
				}
				source.SessionID, source.SessionEpoch = grant.SessionID, grant.SessionEpoch
				return nil
			},
			Authorize: func(source udphub.CenterLocalSource) bool {
				return centerRuntime.Gateway.AuthorizeLocalDevice(localSourceGrant(&source))
			},
			AcquireVoice: func(source udphub.CenterLocalSource) bool {
				return centerRuntime.Gateway.AcquireLocalVoice(localSourceGrant(&source))
			},
			RemoteOwner: centerRuntime.Gateway.IdentityOwnedByRemote,
			Relay: func(source udphub.CenterLocalSource, data []byte) error {
				return centerRuntime.Gateway.RelayLocalDevice(localSourceGrant(&source), data)
			},
			SendConfig: centerRuntime.Gateway.SendDeviceConfig,
			Revoke: func(source udphub.CenterLocalSource) {
				centerRuntime.Gateway.RevokeLocalDevice(source.SessionID, source.SessionEpoch)
			},
		})
		defer udphub.SetCenterInterconnectHooks(udphub.CenterInterconnectHooks{})
		udphub.SetType0Handler(centerRuntime.UDPBridge)
		defer udphub.SetType0Handler(nil)
		stdlog.Printf("Type 0 节点服务已启动: control=%s shared_udp=%s", cfg.Interconnect.ControlListen, cfg.System.Port)
	}

	// 启动 APRS 服务（配置从数据库加载）
	stdlog.Println("正在启动 APRS 服务...")
	aprs.StartAPRSService()

	// 启动 HTTP 服务器（Web API 和前端服务）
	srv := server.New(cfg)
	httpErrCh := make(chan error, 1)
	go func() {
		stdlog.Println("正在启动 HTTP 服务器...")
		if err := srv.Start(); err != nil {
			httpErrCh <- err
		}
	}()

	stdlog.Printf("DraARL v%s 启动成功", buildinfo.VersionString())
	stdlog.Printf("配置: UDP端口=%s, Web端口=%s, MySQL=%s:%d/%s",
		cfg.System.Port, cfg.Web.Port, cfg.Database.Host, cfg.Database.Port, cfg.Database.DBName)

	// 等待信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-quit:
		stdlog.Printf("收到退出信号: %s", sig.String())
	case err := <-httpErrCh:
		stdlog.Printf("HTTP 服务器异常退出: %v", err)
	case err := <-udpErrCh:
		if err != nil {
			stdlog.Printf("UDP 服务器异常退出: %v", err)
		} else {
			stdlog.Printf("UDP 服务器已退出")
		}
	}

	stdlog.Println("正在关闭服务...")

	// 优雅关闭 HTTP 服务
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		stdlog.Printf("HTTP 服务关闭失败: %v", err)
	}
	shutdownCancel()

	// 停止 UDP 服务器
	stdlog.Println("正在停止 UDP 服务器...")
	udphub.StopUDPServer()

	// 停止待绑定设备管理器
	if manager := udphub.GetPendingDeviceManager(); manager != nil {
		manager.Stop()
	}

	// 停止 APRS 服务
	aprs.StopAPRSService()

	// 刷新日志缓冲区
	oplog.Flush()

	stdlog.Println("DraARL 已关闭")
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

// migrateStorageEngine 在源/目标存储引擎间迁移对象（纯命令行，不启动主服务）。
func migrateStorageEngine(spec, configPath string, deleteSource, dryRun bool) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		stdlog.Fatalf("迁移参数格式错误，应为 from:to（如 minio:local 或 minio-prod:r2-prod），已注册驱动: %v", storage.KnownDrivers())
	}
	from := strings.TrimSpace(parts[0])
	to := strings.TrimSpace(parts[1])

	// 加载配置（local 驱动需要 JWT.Secret 初始化签名；此处仅构造驱动，不启动服务）
	cfg, err := config.Load(configPath)
	if err != nil {
		stdlog.Fatalf("加载配置文件失败: %v", err)
	}

	fmt.Println("========================================")
	fmt.Printf("存储迁移: %s -> %s\n", from, to)
	fmt.Printf("模式: dry_run=%t, delete_source=%t\n", dryRun, deleteSource)
	fmt.Println("========================================")

	ctx, cancel := storage.MigrateBackgroundContext()
	defer cancel()

	// 支持 Ctrl+C 中断：已完成的对象不会丢失，重跑可续传
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		stdlog.Println("收到中断信号，正在停止迁移（已完成对象不受影响，重跑可续传）...")
		cancel()
	}()

	res, err := storage.Migrate(ctx, cfg, from, to, storage.MigrateOptions{
		DryRun:       dryRun,
		DeleteSource: deleteSource,
	})
	if res != nil {
		fmt.Println("========================================")
		fmt.Printf("扫描: %d\n", res.Scanned)
		fmt.Printf("复制: %d (%d 字节)\n", res.Copied, res.BytesCopied)
		fmt.Printf("跳过(已存在): %d\n", res.Skipped)
		if deleteSource {
			fmt.Printf("删除源端: %d\n", res.Deleted)
		}
		fmt.Printf("失败: %d\n", res.Failed)
		fmt.Println("========================================")
	}
	if err != nil {
		stdlog.Fatalf("迁移未完成: %v", err)
	}
	fmt.Println("迁移成功完成！")
	if !deleteSource && !dryRun {
		fmt.Println("提示: 源端对象已保留，确认无误后可手动清理或加 -migrate-delete-source 重跑。")
	}
}

// initJWTSecret 初始化JWT密钥，如果不符合要求则自动生成并保存
func initJWTSecret(cfg *config.Configuration) error {
	// 尝试设置JWT密钥
	err := jwt.SetSecret(cfg.JWT.Secret)
	if err == nil {
		stdlog.Println("JWT密钥验证通过")
		return nil
	}

	// 密钥不符合要求，生成新密钥
	stdlog.Printf("JWT密钥不符合要求: %v", err)
	stdlog.Println("正在生成新的JWT密钥...")

	newSecret, genErr := config.GenerateJWTSecret()
	if genErr != nil {
		return fmt.Errorf("生成JWT密钥失败: %w", genErr)
	}

	// 更新配置并保存
	cfg.JWT.Secret = newSecret
	configPath := config.GetConfigPath()
	if saveErr := cfg.SaveToFile(configPath); saveErr != nil {
		return fmt.Errorf("保存配置文件失败: %w", saveErr)
	}

	stdlog.Printf("已生成新的JWT密钥并保存到配置文件: %s", configPath)

	// 使用新密钥
	if setErr := jwt.SetSecret(newSecret); setErr != nil {
		return fmt.Errorf("设置JWT密钥失败: %w", setErr)
	}

	return nil
}
