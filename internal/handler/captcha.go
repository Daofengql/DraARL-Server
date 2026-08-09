package handler

import (
	"net/http"

	"draarl/internal/captcha"

	"github.com/gin-gonic/gin"
)

// VerifyCaptchaCode 验证图片验证码的辅助函数
func VerifyCaptchaCode(captchaID, captchaCode string) bool {
	if captchaID == "" || captchaCode == "" {
		return false
	}
	return captcha.Verify(captchaID, captchaCode, true)
}

// GetCaptcha 获取图片验证码
func GetCaptcha(c *gin.Context) {
	result, err := captcha.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "生成验证码失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    result,
	})
}

// VerifyCaptchaRequest 验证图片验证码请求
type VerifyCaptchaRequest struct {
	CaptchaID  string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// VerifyCaptcha 验证图片验证码（仅用于测试）
func VerifyCaptcha(c *gin.Context) {
	var req VerifyCaptchaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if !captcha.Verify(req.CaptchaID, req.CaptchaCode, true) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "验证码错误或已过期",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "验证成功",
	})
}
