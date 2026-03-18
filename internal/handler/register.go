package handler

import (
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RegUser 注册用户信息
type RegUser struct {
	ID         int       `json:"id"`
	CallSign   string    `json:"callsign"`
	Name       string    `json:"name"`
	Phone      string    `json:"phone"`
	Address    string    `json:"address"`
	Mail       string    `json:"mail"`
	Password   string    `json:"password"`
	OpCertPath string    `json:"op_cert_path"`
	LicensePath string   `json:"license_path"`
	CreateTime string    `json:"create_time"`
	UpdateTime string    `json:"update_time"`
	Status     int       `json:"status"`
	Note       string    `json:"note"`
	ReviewerID int       `json:"reviewer_id"`
}

// Response 统一响应结构
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// GetRegUserList 获取注册用户列表（管理员）
func GetRegUserList(c *gin.Context) {
	// 检查权限
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*UserResponse)
	if !hasAdminRole(userModel.Roles) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "需要管理员权限",
		})
		return
	}

	// TODO: 从数据库查询注册用户列表
	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "成功",
		Data: gin.H{
			"total": 0,
			"items": []RegUser{},
		},
	})
}

// GetRegUserImage 获取注册用户图片
func GetRegUserImage(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*UserResponse)
	if !hasAdminRole(userModel.Roles) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "需要管理员权限",
		})
		return
	}

	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	// 验证路径安全性
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的路径",
		})
		return
	}

	// 检查文件是否存在
	file, err := os.Open(absPath)
	if err != nil {
		c.JSON(http.StatusNotFound, Response{
			Code:    404,
			Message: "文件不存在",
		})
		return
	}
	defer file.Close()

	// 读取文件内容
	imageData, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    500,
			Message: "读取文件失败",
		})
		return
	}

	// 转换为 Base64
	base64Data := base64.StdEncoding.EncodeToString(imageData)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "获取成功",
		Data:    base64Data,
	})
}

// RegisterUser 用户注册（带文件上传）
func RegisterUser(c *gin.Context) {
	if c.Request.Method != http.MethodPost {
		c.JSON(http.StatusMethodNotAllowed, Response{
			Code:    405,
			Message: "仅支持 POST 方法",
		})
		return
	}

	// 限制上传文件大小（10MB）
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)

	// 解析表单数据
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "表单解析失败",
		})
		return
	}

	// 获取表单字段
	callsign := c.PostForm("callsign")
	name := c.PostForm("name")
	phone := c.PostForm("phone")
	address := c.PostForm("address")
	mail := c.PostForm("mail")
	password := c.PostForm("password")

	// 检查必填字段
	if callsign == "" || name == "" || phone == "" || password == "" {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "缺少必填字段",
		})
		return
	}

	// TODO: 检查用户是否已存在
	// user, _ := db.GetUserByPhone(phone)
	// if user != nil {
	// 	c.JSON(http.StatusConflict, Response{
	// 		Code:    409,
	// 		Message: "用户已存在",
	// 	})
	// 	return
	// }

	log.Printf("用户注册: callsign=%s, name=%s, phone=%s", callsign, name, phone)

	_ = RegUser{
		CallSign:   callsign,
		Name:       name,
		Phone:      phone,
		Address:    address,
		Mail:       mail,
		Password:   password,
		CreateTime: time.Now().Format("2006-01-02 15:04:05"),
		UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		Status:     0, // 待审核
	}

	// TODO: 保存到数据库
	// if err := db.CreateRegUser(&regUser); err != nil {
	// 	c.JSON(http.StatusInternalServerError, Response{
	// 		Code:    500,
	// 		Message: "注册失败",
	// 	})
	// 	return
	// }

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "注册成功，请等待管理员审核",
	})
}

// AddRegUser 管理员添加注册用户
func AddRegUser(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*UserResponse)
	if !hasAdminRole(userModel.Roles) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "需要管理员权限",
		})
		return
	}

	var req RegUser
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	req.ReviewerID = userModel.ID
	req.Status = 1 // 已审核通过
	req.CreateTime = time.Now().Format("2006-01-02 15:04:05")
	req.UpdateTime = time.Now().Format("2006-01-02 15:04:05")

	// TODO: 保存到数据库
	// if err := db.CreateRegUser(&req); err != nil {
	// 	c.JSON(http.StatusInternalServerError, Response{
	// 		Code:    500,
	// 		Message: "添加用户失败",
	// 	})
	// 	return
	// }

	log.Printf("管理员添加用户: callsign=%s, name=%s, reviewer=%d", req.CallSign, req.Name, req.ReviewerID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "添加成功",
	})
}

// UpdateRegUser 更新注册用户
func UpdateRegUser(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*UserResponse)
	if !hasAdminRole(userModel.Roles) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "需要管理员权限",
		})
		return
	}

	var req RegUser
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	req.UpdateTime = time.Now().Format("2006-01-02 15:04:05")

	// TODO: 更新数据库
	// if err := db.UpdateRegUser(&req); err != nil {
	// 	c.JSON(http.StatusInternalServerError, Response{
	// 		Code:    500,
	// 		Message: "更新失败",
	// 	})
	// 	return
	// }

	log.Printf("管理员更新用户: callsign=%s, status=%d", req.CallSign, req.Status)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "更新成功",
	})
}

// DeleteRegUser 删除注册用户
func DeleteRegUser(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*UserResponse)
	if !hasAdminRole(userModel.Roles) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "需要管理员权限",
		})
		return
	}

	var req RegUser
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "请求参数错误",
		})
		return
	}

	// TODO: 从数据库删除
	// if err := db.DeleteRegUser(req.ID); err != nil {
	// 	c.JSON(http.StatusInternalServerError, Response{
	// 		Code:    500,
	// 		Message: "删除失败",
	// 	})
	// 	return
	// }

	log.Printf("管理员删除用户: callsign=%s", req.CallSign)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "删除成功",
	})
}

// saveUploadedFile 保存上传的文件
func saveUploadedFile(file io.Reader, path string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, file)
	return err
}

// hasAdminRole 检查是否有管理员角色
func hasAdminRole(roles []string) bool {
	for _, role := range roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

// ApproveRegUser 审核通过注册用户
func ApproveRegUser(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*UserResponse)
	if !hasAdminRole(userModel.Roles) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "需要管理员权限",
		})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的用户 ID",
		})
		return
	}

	// TODO: 更新数据库状态为通过
	// if err := db.ApproveRegUser(id, userModel.ID); err != nil {
	// 	c.JSON(http.StatusInternalServerError, Response{
	// 		Code:    500,
	// 		Message: "审核失败",
	// 	})
	// 	return
	// }

	log.Printf("管理员审核通过用户 ID: %d, reviewer: %d", id, userModel.ID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "审核成功",
	})
}

// RejectRegUser 审核拒绝注册用户
func RejectRegUser(c *gin.Context) {
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, Response{
			Code:    401,
			Message: "未授权",
		})
		return
	}

	userModel := user.(*UserResponse)
	if !hasAdminRole(userModel.Roles) {
		c.JSON(http.StatusForbidden, Response{
			Code:    403,
			Message: "需要管理员权限",
		})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    400,
			Message: "无效的用户 ID",
		})
		return
	}

	// TODO: 更新数据库状态为拒绝
	// if err := db.RejectRegUser(id, userModel.ID); err != nil {
	// 	c.JSON(http.StatusInternalServerError, Response{
	// 		Code:    500,
	// 		Message: "操作失败",
	// 	})
	// 	return
	// }

	log.Printf("管理员审核拒绝用户 ID: %d, reviewer: %d", id, userModel.ID)

	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "已拒绝",
	})
}
