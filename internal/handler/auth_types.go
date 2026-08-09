package handler

import gormdb "draarl/internal/gormdb"

// LoginRequest 登录请求。
type LoginRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

// RegisterRequest 注册请求。
type RegisterRequest struct {
	Username  string `json:"username" binding:"required"`
	Password  string `json:"password" binding:"required"`
	CallSign  string `json:"callsign" binding:"required"`
	Phone     string `json:"phone"`
	NickName  string `json:"nickname"`
	Email     string `json:"email" binding:"required,email"`
	SessionID string `json:"session_id"`
	EmailCode string `json:"email_code"`
}

// UserResponse 用户响应（用于中间件传递）。
type UserResponse struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	CallSign string   `json:"callsign"`
	Roles    []string `json:"roles"`
}

type UpdateUserRequest struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	NickName string `json:"nickname"`
	CallSign string `json:"callsign"`
	Phone    string `json:"phone"`
	Address  string `json:"address"`
	Status   int    `json:"status"`
	Roles    string `json:"roles"`
	Role     string `json:"role"`
}

type UpdateUserStatusRequest struct {
	Status int `json:"status"`
}

type UpdateProfileRequest struct {
	NickName     string `json:"nickname"`
	Phone        string `json:"phone"`
	Address      string `json:"address"`
	Introduction string `json:"introduction"`
	Sex          *int   `json:"sex"`
	Birthday     string `json:"birthday"`
	DMRID        *int   `json:"dmrid"`
	MDCID        string `json:"mdcid"`
	AlarmMsg     *bool  `json:"alarm_msg"`
}

type UpdateUserPasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

type ChangeOwnPasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

type TotalStats struct {
	TotalDevices  int64 `json:"total_devices"`
	OnlineDevices int64 `json:"online_devices"`
	TotalUsers    int64 `json:"total_users"`
	TotalGroups   int64 `json:"total_groups"`
}

func hasRoleGORM(user *gormdb.User, role string) bool {
	if user.Roles == "" {
		return role == "user"
	}
	return user.Roles == role
}

func getRoleName(roles []string) string {
	for _, role := range roles {
		if role == "admin" {
			return "admin"
		}
	}
	return "user"
}

func getRoleNameFromUser(user *gormdb.User) string {
	if user.Roles == "" {
		return "user"
	}
	if hasRoleGORM(user, "admin") {
		return "admin"
	}
	return "user"
}
