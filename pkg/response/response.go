package response

import (
	"encoding/json"
	"net/http"
)

// Response 标准响应结构
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// 响应码常量
const (
	CodeOK              = 20000 // 操作成功
	CodeOpError         = 20001 // 操作失败
	CodeRightError      = 20002 // 权限不足
	CodeParamError      = 20003 // 参数错误
	CodeUserExists      = 20004 // 用户已经存在
	CodeTokenError      = 50008 // 令牌错误
	CodeTokenFormatErr  = 50009 // 格式错误
	CodeTokenSignErr    = 50010 // 签名错误
	CodeTokenDecodeErr  = 50011 // 解码错误
	CodeTokenTimeoutErr = 50012 // 登录超时
	CodeTokenExpireErr  = 50013 // 登录过期
)

// 预定义响应消息
var (
	ResOK              = mustMarshal(Response{CodeOK, "操作成功", nil})
	ResOpErr           = mustMarshal(Response{CodeOpError, "操作失败", nil})
	ResRightErr        = mustMarshal(Response{CodeRightError, "权限不足", nil})
	ResParamErr        = mustMarshal(Response{CodeParamError, "参数错误", nil})
	ResUserAlreadyExit = mustMarshal(Response{CodeUserExists, "用户已经存在", nil})

	ResTokenErr       = mustMarshal(Response{CodeTokenError, "令牌错误，可能是登录超时，请重新登录", nil})
	ResTokenFormatErr = mustMarshal(Response{CodeTokenFormatErr, "格式错误", nil})
	ResTokenSignErr   = mustMarshal(Response{CodeTokenSignErr, "签名错误", nil})
	ResTokenDecodeErr = mustMarshal(Response{CodeTokenDecodeErr, "解码错误", nil})
	ResTokenTimeoutErr = mustMarshal(Response{CodeTokenTimeoutErr, "登录超时", nil})
	ResTokenExpireErr  = mustMarshal(Response{CodeTokenExpireErr, "登录过期", nil})
)

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ItemsData 带总数的列表数据
type ItemsData struct {
	Total int    `json:"total"`
	Items any    `json:"items"`
}

// JSON 写入 JSON 响应
func JSON(w http.ResponseWriter, code int, message string, data any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(Response{code, message, data})
}

// OK 成功响应
func OK(w http.ResponseWriter, data any) error {
	return JSON(w, CodeOK, "ok", data)
}

// Error 错误响应
func Error(w http.ResponseWriter, code int, message string) error {
	return JSON(w, code, message, nil)
}

// Items 带总数的列表响应
func Items(w http.ResponseWriter, items any, total int) error {
	return JSON(w, CodeOK, "ok", ItemsData{Total: total, Items: items})
}

// Item 单个对象响应
func Item(w http.ResponseWriter, item any) error {
	return JSON(w, CodeOK, "ok", item)
}

// WriteBytes 写入原始字节响应
func WriteBytes(w http.ResponseWriter, data []byte) error {
	w.Header().Set("Content-Type", "application/json")
	_, err := w.Write(data)
	return err
}

// SetHeaders 设置通用响应头
func SetHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}
