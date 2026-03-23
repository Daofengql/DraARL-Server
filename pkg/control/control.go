package control

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Control 设备控制参数结构
type Control struct {
	DCDSelect         byte   `json:"dcd_select"`          // 0x00  DCD 0=PTT DISABLE   1=MANUAL  2=SQL_LO  3=SQL_HI    4=VOX
	PTTEnable         byte   `json:"ptt_enable"`          // 0x01  0=PTT DISABLE   1=PTT ENABLE
	PTTLevelReversed  byte   `json:"ptt_level_reversed"`  // 0x02  PTT电平反转
	AddTailVoice      uint16 `json:"add_tail_voice"`      // 0x03-0x04  默认加尾音20   步进5ms,最小大于20*5=100ms
	RemoveTailVoice   uint16 `json:"remove_tail_voice"`   // 0x05-0x06  默认消尾音,步进5MS  50*5=250ms
	PTTresistive      byte   `json:"ptt_resistive"`       // 0x07  PTT 电阻  0=0FF 1=EN
	Monitor           byte   `json:"monitor"`             // 0x08  MONITOR 监听输出  0=0FF 1=EN
	KeyFunc           byte   `json:"key_func"`            // 0x09  自定义KEY  0=Relay 1=MANUAL PTT
	RealyStatus       byte   `json:"realy_status"`        // 0x0A  Relay继电器掉电状态 0=断开  1=吸合
	AllowRealyControl byte   `json:"allow_relay_control"` // 0x0B  是否允许继电器控制
	VoiceBitrate      byte   `json:"voice_bitrate"`       // 0x0C  H=原码率  M=码率/2
	DMRID             string `json:"dmrid"`               // 0x10-0x19  本机设备序列号，不可修改
	Password          string `json:"password"`            // 0x1A-0x1E  本机设备密码，不可修改
	InitSign          byte   `json:"init_sign"`           // 0x1F  初始化标记
	LocalIPaddr       string `json:"local_ipaddr"`        // 0x20-0x23  192.168.1.190
	Gateway           string `json:"gateway"`             // 0x24-0x27  192.168.1.1
	NetMask           string `json:"netmask"`             // 0x28-0x31  255.255.255.0
	DNSIP             string `json:"dns_ipaddr"`          // 0x2C-0x2F  114.114.114.114
	DestPort          uint16 `json:"dest_port"`           // 0x30-0x31  UDP AUDIO OUT目标端口号
	LoaclPort         uint16 `json:"local_port"`          // 0x32-0x33  UDP AUDIO IN本机端口号
	SSID              byte   `json:"ssid"`                // 0x40
	CallSign          string `json:"callsign"`            // 0x41-0x47  呼号 最长6位 0X00结束符号
	DestDomainName    string `json:"dest_domainname"`     // 0x50-0x7F  目标IP或域名

	// 1W 参数
	OneGBWBand        byte   `json:"one_band"`
	OneGBWDTMF        byte   `json:"one_dtmf"`
	OneReciveFreq     string `json:"one_recive_freq"`
	OneTransmitFreq   string `json:"one_transmit_freq"`
	OneReciveCXCSS    string `json:"one_recive_cxcss"`
	OneTransmitCXCSS  string `json:"one_transmit_cxcss"`
	OneSQLLevel       int    `json:"one_sql_level"`
	OneVolume         int    `json:"one_volume"`          // 0xA0  UV1模块音量1-9级
	OneMICSensitivity int    `json:"one_mic_sensitivity"` // 0xA1  MIC灵敏度1-8
	OneMICEncryption  int    `json:"one_mic_encryption"`  // 0xA2  MIC语音加密 0 1-8
	OneUVPower        byte   `json:"one_uv_power"`        // 0xA3 PD 内置UV模块电源开关

	// Moto 3188 3688 信道
	MotoChannel byte `json:"moto_channel"`

	// 2W 参数
	TwoReciveFreq    string `json:"two_recive_freq"`    // 0xC0-0xC8
	TwoTransmitFreq  string `json:"two_transmit_freq"`  // 0xCA-0xD3
	TwoReciveCXCSS   string `json:"two_recive_cxcss"`   // 0xD4-0xD8
	TwoTransmitCXCSS string `json:"two_transmit_cxcss"` // 0xDA-0xDE
	FLAG1            string `json:"flag1"`              // 0xE0
	FLAG2            string `json:"flag2"`              // 0xE2
	TwoVolume        int    `json:"two_volume"`         // 0xEE 2W音量
	TwoSavePower     int    `json:"two_save_power"`     // 0xEF 2W SAVE
	TwoSQLLevel      int    `json:"two_sql_level"`      // 0xF0
	TwoMICLevel      int    `json:"two_mic_level"`      // 0xF2
	TwoTOTLevel      int    `json:"two_tot_level"`      // 0xF4

	Data []byte // 原始数据
}

// ATCommand AT命令结构
type ATCommand struct {
	CallSign  string            `json:"callsign"`
	SSID      byte              `json:"ssid"`
	Type      byte              `json:"type"` // 0x01 查询AT   0x02 写入AT
	ATcommand string            `json:"atcommand"`
	Data      string            `json:"data"`
	Version   string            `json:"version"`
	ATMap     map[string]string `json:"atmap"`
}

func (a *ATCommand) String() string {
	return fmt.Sprintf("%s=%s\r\n", a.ATcommand, a.Data)
}

// DecodeATPacket 解码AT命令包
func DecodeATPacket(callsign string, ssid byte, data []byte) *ATCommand {
	c := &ATCommand{CallSign: callsign, SSID: ssid}

	if len(data) < 2 {
		log.Println("AT command error:", callsign, ssid, data)
		c.Version = "DraARL AT ERROR"
		return c
	}

	c.Type = data[0]

	if c.Type == 0x02 {
		c.ATMap = make(map[string]string)
		for v := range strings.SplitSeq(string(data[1:]), "\r\n") {
			if strings.HasPrefix(v, "DraARL") {
				c.Version = v
				continue
			}

			kv := strings.SplitN(v, "=", 2)
			if len(kv) != 2 {
				continue
			}
			c.ATMap[kv[0]] = kv[1]
		}
	} else {
		log.Printf("AT command type error: %s %d %d %s %v", callsign, ssid, c.Type, string(data[1:]), data)
	}

	return c
}

// DecodeControlPacket 解码控制参数包
func DecodeControlPacket(data []byte) *Control {
	c := &Control{}

	// 子类型为2是响应
	if data[0] == 2 && len(data) > 512 {
		c.Data = make([]byte, 512)
		copy(c.Data, data[1:])

		c.DCDSelect = c.Data[0]
		c.PTTEnable = c.Data[1]
		c.PTTLevelReversed = c.Data[2]
		c.AddTailVoice = uint16(c.Data[3])<<8 | uint16(c.Data[4])
		c.RemoveTailVoice = uint16(c.Data[5])<<8 | uint16(c.Data[6])
		c.PTTresistive = c.Data[7]
		c.Monitor = c.Data[8]
		c.KeyFunc = c.Data[9]
		c.RealyStatus = c.Data[10]
		c.AllowRealyControl = c.Data[11]
		c.VoiceBitrate = c.Data[12]
		c.DMRID = string(c.Data[16:26])
		c.Password = string(c.Data[26:31])
		c.InitSign = c.Data[31]
		c.LocalIPaddr = fmt.Sprintf("%v.%v.%v.%v", c.Data[32], c.Data[33], c.Data[34], c.Data[35])
		c.Gateway = fmt.Sprintf("%v.%v.%v.%v", c.Data[36], c.Data[37], c.Data[38], c.Data[39])
		c.NetMask = fmt.Sprintf("%v.%v.%v.%v", c.Data[40], c.Data[41], c.Data[42], c.Data[43])
		c.DNSIP = fmt.Sprintf("%v.%v.%v.%v", c.Data[44], c.Data[45], c.Data[46], c.Data[47])
		c.DestPort = uint16(c.Data[48])<<8 | uint16(c.Data[49])
		c.LoaclPort = uint16(c.Data[50])<<8 | uint16(c.Data[51])
		c.SSID = c.Data[64]
		c.CallSign = string(bytes.Split(c.Data[65:72], []byte{0x00})[0])
		c.DestDomainName = string(bytes.Split(c.Data[80:128], []byte{0x00})[0])

		// 1W 参数解析
		oneParm := bytes.Split(bytes.Split(c.Data[128:160], []byte{0x00})[0], []byte{','})
		if len(oneParm) >= 6 {
			if s, err := strconv.Atoi(string(oneParm[0])); err == nil {
				c.OneGBWBand = byte(s) & 0x01
				c.OneGBWDTMF = byte(s) & 0x02
			}
			c.OneTransmitFreq = string(oneParm[1])
			c.OneReciveFreq = string(oneParm[2])
			c.OneReciveCXCSS = string(oneParm[3])
			c.OneSQLLevel, _ = strconv.Atoi(string(oneParm[4]))
			c.OneTransmitCXCSS = string(oneParm[5])
		}

		c.OneVolume, _ = strconv.Atoi(string(c.Data[160]))
		c.OneMICSensitivity, _ = strconv.Atoi(string(c.Data[161]))
		c.OneMICEncryption, _ = strconv.Atoi(string(c.Data[162]))
		c.OneUVPower = c.Data[163]

		// Moto 3188
		c.MotoChannel = c.Data[164]

		// 2W 参数解析
		twoParm := bytes.Split(bytes.Split(c.Data[192:227], []byte{0x00})[0], []byte{','})
		if len(twoParm) >= 6 {
			c.TwoReciveFreq = string(twoParm[0])
			c.TwoTransmitFreq = string(twoParm[1])
			c.TwoReciveCXCSS = string(twoParm[2])
			c.TwoTransmitCXCSS = string(twoParm[3])
			c.FLAG1 = string(twoParm[4])
			c.FLAG2 = string(twoParm[5])
		}

		c.TwoVolume, _ = strconv.Atoi(string(c.Data[238]))
		c.TwoSavePower, _ = strconv.Atoi(string(c.Data[239]))
		c.TwoSQLLevel, _ = strconv.Atoi(string(c.Data[240]))
		c.TwoMICLevel, _ = strconv.Atoi(string(c.Data[242]))
		c.TwoTOTLevel, _ = strconv.Atoi(string(c.Data[244]))
	}

	return c
}

// EncodeControlPacket 编码控制参数包
func EncodeControlPacket(subtype byte, data []byte) []byte {
	packet := make([]byte, 1+len(data))
	packet[0] = subtype
	copy(packet[1:], data)
	return packet
}
