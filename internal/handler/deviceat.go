package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"nrllink/internal/db"
)

// ATCommand AT命令结构
type ATCommand struct {
	CallSign  string            `json:"callsign" binding:"required"`
	SSID      byte              `json:"ssid"`
	Type      byte              `json:"type"` // 0x01 查询AT   0x02 写入AT
	ATCommand string            `json:"atcommand"`
	Data      string            `json:"data"`
	Version   string            `json:"version"`
	ATMap     map[string]string `json:"atmap"`
}

// ControlPacket 设备控制参数结构（512字节）
type ControlPacket struct {
	DCDSelect         byte   `json:"dcd_select"`          // 0x00
	PTTEnable         byte   `json:"ptt_enable"`          // 0x01
	PTTLevelReversed  byte   `json:"ptt_level_reversed"`  // 0x02
	AddTailVoice      uint16 `json:"add_tail_voice"`      // 0x03-0x04
	RemoveTailVoice   uint16 `json:"remove_tail_voice"`   // 0x05-0x06
	PTTresistive      byte   `json:"ptt_resistive"`       // 0x07
	Monitor           byte   `json:"monitor"`             // 0x08
	KeyFunc           byte   `json:"key_func"`            // 0x09
	RealyStatus       byte   `json:"realy_status"`        // 0x0A
	AllowRealyControl byte   `json:"allow_relay_control"` // 0x0B
	VoiceBitrate      byte   `json:"voice_bitrate"`       // 0x0C
	DMRID             string `json:"dmrid"`               // 0x10-0x19
	Password          string `json:"password"`            // 0x1A-0x1E
	LocalIPaddr       string `json:"local_ipaddr"`        // 0x20-0x23
	Gateway           string `json:"gateway"`             // 0x24-0x27
	NetMask           string `json:"netmask"`             // 0x28-0x2B
	DNSIP             string `json:"dns_ipaddr"`          // 0x2C-0x2F
	LoaclPort         uint16 `json:"local_port"`          // 0x32-0x33
	SSID              byte   `json:"ssid"`                // 0x40
	CallSign          string `json:"callsign"`            // 0x41-0x47
	DestDomainName    string `json:"dest_domainname"`    // 0x50-0x7F
	OneGBWBand        byte   `json:"one_band"`
	OneGBWDTMF        byte   `json:"one_dtmf"`
	OneReciveFreq     string `json:"one_recive_freq"`
	OneTransmitFreq   string `json:"one_transmit_freq"`
	OneReciveCXCSS    string `json:"one_recive_cxcss"`
	OneTransmitCXCSS  string `json:"one_transmit_cxcss"`
	OneSQLLevel       int    `json:"one_sql_level"`
	OneVolume         int    `json:"one_volume"`          // 0xA0
	OneMICSensitivity int    `json:"one_mic_sensitivity"` // 0xA1
	OneMICEncryption  int    `json:"one_mic_encryption"`  // 0xA2
	OneUVPower        byte   `json:"one_uv_power"`        // 0xA3
	MotoChannel       byte   `json:"moto_channel"`
	TwoReciveFreq     string `json:"two_recive_freq"`     // 0xC0-0xC8
	TwoTransmitFreq   string `json:"two_transmit_freq"`   // 0xCA-0xD3
	TwoReciveCXCSS    string `json:"two_recive_cxcss"`    // 0xD4-0xD8
	TwoTransmitCXCSS  string `json:"two_transmit_cxcss"` // 0xDA-0xDE
	FLAG1             string `json:"flag1"`               // 0xE0
	FLAG2             string `json:"flag2"`               // 0xE2
	TwoVolume         int    `json:"two_volume"`          // 0xEE
	TwoSavePower      int    `json:"two_save_power"`      // 0xEF
	TwoSQLLevel       int    `json:"two_sql_level"`       // 0xF0
	TwoMICLevel       int    `json:"two_mic_level"`       // 0xF2
	TwoTOTLevel       int    `json:"two_tot_level"`       // 0xF4
}

// DeviceAT 执行设备AT命令
func DeviceAT(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	var req ATCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "AT数据格式错误",
			},
		})
		return
	}

	// 获取用户信息
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "获取用户信息失败",
			},
		})
		return
	}

	// 检查权限
	isAdmin := false
	if roleList, ok := roles.([]string); ok {
		for _, r := range roleList {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
	}

	if !isAdmin && user.CallSign != req.CallSign {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "修改设备信息错误，不是本人，或者权限不够！",
			},
		})
		return
	}

	// TODO: 通过 UDP Hub 发送 AT 命令到设备
	// 这里需要调用 udphub 包中的函数来发送命令

	// 模拟返回
	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"message": "AT命令执行成功",
			"items": gin.H{
				"callsign": req.CallSign,
				"ssid":     req.SSID,
				"version":  "NRL AT V1.0",
			},
		},
	})
}

// QueryDeviceParm 查询设备参数
func QueryDeviceParm(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	var req struct {
		CallSign string `json:"callsign" binding:"required"`
		SSID     byte   `json:"ssid"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "查询设备信息错误",
			},
		})
		return
	}

	// 获取用户信息
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "获取用户信息失败",
			},
		})
		return
	}

	// 检查权限
	isAdmin := false
	if roleList, ok := roles.([]string); ok {
		for _, r := range roleList {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
	}

	if !isAdmin && user.CallSign != req.CallSign {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "修改设备信息错误",
			},
		})
		return
	}

	// TODO: 通过 UDP Hub 发送参数查询命令到设备
	// 这里需要调用 udphub 包中的函数来查询设备参数

	// 模拟返回
	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"items": gin.H{
				"callsign":        req.CallSign,
				"ssid":            req.SSID,
				"dcd_select":      0,
				"ptt_enable":      1,
				"ptt_level_reversed": 0,
				"add_tail_voice":   100,
				"remove_tail_voice": 250,
			},
		},
	})
}

// ChangeDeviceParm 修改设备参数
func ChangeDeviceParm(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	// 从表单或 JSON 获取参数
	var req struct {
		CallSign string `form:"callsign" json:"callsign"`
		SSID     string `form:"ssid" json:"ssid"`
	}
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "参数错误",
			},
		})
		return
	}

	// 获取用户信息
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "获取用户信息失败",
			},
		})
		return
	}

	// 检查权限
	isAdmin := false
	if roleList, ok := roles.([]string); ok {
		for _, r := range roleList {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
	}

	if !isAdmin && user.CallSign != req.CallSign {
		c.JSON(http.StatusOK, gin.H{
			"code": 20001,
			"data": gin.H{
				"message": "权限不够",
			},
		})
		return
	}

	// TODO: 解析各种参数并发送到设备
	// 支持的参数：dcd_select, ptt_enable, ptt_level_reversed, add_tail_voice,
	// remove_tail_voice, ptt_resistive, monitor, key_func, realy_status,
	// allow_relay_control, voice_bitrate, local_ipaddr, gateway, netmask,
	// dns_ipaddr, dest_domainname, newcallsignssid, one_uv_power,
	// moto_channel, one_transmit_freq, one_recive_freq 等

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"message": "修改成功",
		},
	})
}

// Change1W 修改1W模块参数
func Change1W(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	var req ControlPacket
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "1W设备参数信息更新错误,数据格式错误",
			},
		})
		return
	}

	// 获取用户信息
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "获取用户信息失败",
			},
		})
		return
	}

	// 检查权限
	isAdmin := false
	if roleList, ok := roles.([]string); ok {
		for _, r := range roleList {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
	}

	if !isAdmin && user.CallSign != req.CallSign {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "修改设备信息错误，不是本人，或者权限不够！",
			},
		})
		return
	}

	// TODO: 通过 UDP Hub 发送 1W 模块参数修改命令到设备

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"message": "1W模块设置完成",
		},
	})
}

// Change2W 修改2W模块参数
func Change2W(c *gin.Context) {
	username, _ := c.Get("username")
	roles, _ := c.Get("roles")

	var req ControlPacket
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "2W设备参数信息更新错误,数据格式错误",
			},
		})
		return
	}

	// 获取用户信息
	user, err := db.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "获取用户信息失败",
			},
		})
		return
	}

	// 检查权限
	isAdmin := false
	if roleList, ok := roles.([]string); ok {
		for _, r := range roleList {
			if r == "admin" {
				isAdmin = true
				break
			}
		}
	}

	if !isAdmin && user.CallSign != req.CallSign {
		c.JSON(http.StatusOK, gin.H{
			"code": 20000,
			"data": gin.H{
				"message": "修改设备信息错误，不是本人，或者权限不够！",
			},
		})
		return
	}

	// TODO: 通过 UDP Hub 发送 2W 模块参数修改命令到设备

	c.JSON(http.StatusOK, gin.H{
		"code": 20000,
		"data": gin.H{
			"message": "2W模块设置成功",
		},
	})
}
