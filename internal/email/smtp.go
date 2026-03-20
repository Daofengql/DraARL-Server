package email

import (
	"crypto/tls"
	"fmt"
	"log"

	"gopkg.in/gomail.v2"

	gormdb "nrllink/internal/gormdb"
)

// SMTPService SMTP邮件服务
type SMTPService struct {
	config *gormdb.SMTPConfig
}

// NewSMTPService 创建SMTP服务
func NewSMTPService() *SMTPService {
	repo := gormdb.GetSiteConfigRepo()
	config, err := repo.GetSMTPConfig()
	if err != nil {
		log.Printf("获取SMTP配置失败: %v", err)
		return &SMTPService{config: &gormdb.SMTPConfig{}}
	}
	return &SMTPService{config: config}
}

// RefreshConfig 刷新配置
func (s *SMTPService) RefreshConfig() {
	repo := gormdb.GetSiteConfigRepo()
	config, err := repo.GetSMTPConfig()
	if err != nil {
		log.Printf("刷新SMTP配置失败: %v", err)
		return
	}
	s.config = config
}

// IsConfigured 检查是否已配置SMTP
func (s *SMTPService) IsConfigured() bool {
	return s.config != nil &&
		s.config.Host != "" &&
		s.config.Port > 0 &&
		s.config.SenderEmail != "" &&
		s.config.Password != ""
}

// SendVerificationCode 发送验证码邮件
func (s *SMTPService) SendVerificationCode(toEmail, code, purpose string) error {
	if !s.IsConfigured() {
		return fmt.Errorf("SMTP未配置")
	}

	var subject, body string
	switch purpose {
	case "register":
		subject = "注册验证码"
		body = fmt.Sprintf(`
			<h2>欢迎注册</h2>
			<p>您的注册验证码是：<strong style="font-size:24px;color:#1976d2;">%s</strong></p>
			<p>验证码有效期为10分钟，请尽快完成验证。</p>
			<p>如果这不是您本人的操作，请忽略此邮件。</p>
		`, code)
	case "login":
		subject = "登录验证码"
		body = fmt.Sprintf(`
			<h2>登录验证</h2>
			<p>您的登录验证码是：<strong style="font-size:24px;color:#1976d2;">%s</strong></p>
			<p>验证码有效期为10分钟，请尽快完成验证。</p>
			<p>如果这不是您本人的操作，请立即修改密码。</p>
		`, code)
	case "reset_password":
		subject = "重置密码验证码"
		body = fmt.Sprintf(`
			<h2>重置密码</h2>
			<p>您正在申请重置密码，验证码是：<strong style="font-size:24px;color:#1976d2;">%s</strong></p>
			<p>验证码有效期为10分钟，请尽快完成验证。</p>
			<p>如果这不是您本人的操作，请忽略此邮件。</p>
		`, code)
	default:
		subject = "验证码"
		body = fmt.Sprintf(`
			<h2>验证码</h2>
			<p>您的验证码是：<strong style="font-size:24px;color:#1976d2;">%s</strong></p>
			<p>验证码有效期为10分钟，请尽快完成验证。</p>
		`, code)
	}

	return s.SendMail(toEmail, subject, body)
}

// SendMail 发送邮件
func (s *SMTPService) SendMail(to, subject, body string) error {
	if !s.IsConfigured() {
		return fmt.Errorf("SMTP未配置")
	}

	m := gomail.NewMessage()
	m.SetAddressHeader("From", s.config.SenderEmail, s.config.SenderName)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body)

	d := gomail.NewDialer(s.config.Host, s.config.Port, s.config.SenderEmail, s.config.Password)

	// 如果使用SSL，设置TLS配置
	if s.config.UseSSL {
		d.SSL = true
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if err := d.DialAndSend(m); err != nil {
		log.Printf("发送邮件失败: %v", err)
		return fmt.Errorf("发送邮件失败: %v", err)
	}

	return nil
}
