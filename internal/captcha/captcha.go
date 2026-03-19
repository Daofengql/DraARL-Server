package captcha

import (
	"time"

	"github.com/mojocn/base64Captcha"
)

// CaptchaResult 验证码结果
type CaptchaResult struct {
	ID     string `json:"captcha_id"`
	Image  string `json:"captcha_image"` // base64编码的图片
	Expire int    `json:"expire"`        // 有效期(秒)
}

// CaptchaService 验证码服务
type CaptchaService struct {
	store  base64Captcha.Store
	driver *base64Captcha.DriverString
	expire time.Duration
}

var (
	// 全局验证码服务实例
	captchaService *CaptchaService
)

// Init 初始化验证码服务
func Init() {
	// 创建内存存储（验证码过期时间300秒，GC间隔60秒）
	store := base64Captcha.NewMemoryStore(300, 60)

	// 创建驱动配置
	driver := &base64Captcha.DriverString{
		Height:          80,
		Width:           240,
		NoiseCount:      5,
		ShowLineOptions: base64Captcha.OptionShowSlimeLine | base64Captcha.OptionShowHollowLine,
		Length:          5,
		Source:          "1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
		BgColor:         nil, // 使用默认背景色
		Fonts:           nil, // 使用默认字体
	}

	captchaService = &CaptchaService{
		store:  store,
		driver: driver,
		expire: 5 * time.Minute,
	}
}

// Generate 生成验证码
func Generate() (*CaptchaResult, error) {
	if captchaService == nil {
		Init()
	}

	// 创建验证码
	captcha := base64Captcha.NewCaptcha(captchaService.driver.ConvertFonts(), captchaService.store)
	id, b64s, _, err := captcha.Generate()
	if err != nil {
		return nil, err
	}

	return &CaptchaResult{
		ID:     id,
		Image:  b64s,
		Expire: int(captchaService.expire.Seconds()),
	}, nil
}

// Verify 验证验证码
// clear: 验证成功后是否清除
func Verify(id, answer string, clear bool) bool {
	if captchaService == nil {
		Init()
	}
	return captchaService.store.Verify(id, answer, clear)
}
