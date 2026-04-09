"""
DraARLv1 协议定义和编解码
"""

import struct
from dataclasses import dataclass
from typing import Optional, List

# ==========================================
# 协议常量
# ==========================================
DRAARL_VERSION = b"DraA"
DRAARL_HEADER_SIZE = 90
DRAARL_MAX_PACKET_SIZE = 800

# 数据包类型
class PacketType:
    CONTROL = 0        # 控制指令
    JWT_AUTH = 1       # JWT 认证包
    HEARTBEAT = 2      # 心跳包
    CONFIG = 3         # 设备配置
    TEXT_MESSAGE = 4   # 文本消息
    OPUS_16K = 5       # Opus 16K 语音
    SERVER_VOICE = 6   # 服务器互联语音
    AT_PASSTHROUGH = 7 # AT 透传

# 设备型号
class DevModel:
    UNKNOWN = 0
    WECHAT_MINI = 100   # 微信小程序
    ANDROID = 101       # Android 客户端
    IOS = 102           # iOS 客户端
    WINDOWS = 103       # Windows 客户端
    MACOS = 104         # macOS 客户端
    BROWSER = 105       # 浏览器客户端
    INTERCONNECT = 106  # 互联设备

# JWT 认证响应状态码
class JWTAuthStatus:
    SUCCESS = 0              # 认证成功
    INVALID_TOKEN = 1        # Token 无效或过期
    USER_NOT_FOUND = 2       # 用户不存在
    USER_DISABLED = 3        # 用户已禁用
    USER_NOT_APPROVED = 4    # 用户未审核
    INVALID_DEV_MODEL = 5    # 无效的设备型号

# SSID 范围
class SSIDRange:
    NORMAL1_MIN = 1
    NORMAL1_MAX = 99
    NORMAL2_MIN = 106
    NORMAL2_MAX = 235
    GHOST_MIN = 100
    GHOST_MAX = 105
    INTERCONNECT_MIN = 236
    INTERCONNECT_MAX = 255


class DraARLv1Packet:
    """DraARLv1 协议数据包"""

    def __init__(self):
        self.version = DRAARL_VERSION
        self.length = DRAARL_HEADER_SIZE
        self.username = ""
        self.device_password = ""
        self.packet_type = 0
        self.dev_model = 0
        self.ssid = 0
        self.dmrid = 0
        self.callsign = ""
        self.reserved = b'\x00' * 4
        self.data = b''

    @classmethod
    def decode(cls, raw_data: bytes) -> Optional['DraARLv1Packet']:
        """解码原始数据为协议包"""
        if len(raw_data) < DRAARL_HEADER_SIZE:
            return None

        try:
            packet = cls()

            # 解析头部
            packet.version = raw_data[0:4]
            if packet.version != DRAARL_VERSION:
                return None

            packet.length = struct.unpack('>H', raw_data[4:6])[0]
            packet.username = raw_data[6:38].rstrip(b'\x00').decode('utf-8', errors='replace')
            packet.device_password = raw_data[38:48].rstrip(b'\x00').decode('ascii', errors='replace')
            packet.packet_type = raw_data[48]
            packet.dev_model = raw_data[49]
            packet.ssid = raw_data[50]
            packet.dmrid = (raw_data[51] << 16) | (raw_data[52] << 8) | raw_data[53]
            packet.callsign = raw_data[54:86].rstrip(b'\x00').decode('ascii', errors='replace')
            packet.reserved = raw_data[86:90]

            # 解析数据区
            if len(raw_data) > DRAARL_HEADER_SIZE:
                packet.data = raw_data[DRAARL_HEADER_SIZE:]

            return packet
        except Exception:
            return None

    def encode(self) -> bytes:
        """编码为原始字节数据"""
        total_size = DRAARL_HEADER_SIZE + len(self.data)
        packet = bytearray(total_size)

        # Version (0-3)
        packet[0:4] = self.version

        # Length (4-5)
        struct.pack_into('>H', packet, 4, total_size)

        # Username (6-37)
        username_bytes = self.username.encode('utf-8')[:32].ljust(32, b'\x00')
        packet[6:38] = username_bytes

        # DevicePassword (38-47)
        password_bytes = self.device_password.encode('ascii')[:10].ljust(10, b'\x00')
        packet[38:48] = password_bytes

        # Type (48)
        packet[48] = self.packet_type

        # DevModel (49)
        packet[49] = self.dev_model

        # SSID (50)
        packet[50] = self.ssid

        # DMRID (51-53)
        packet[51] = (self.dmrid >> 16) & 0xFF
        packet[52] = (self.dmrid >> 8) & 0xFF
        packet[53] = self.dmrid & 0xFF

        # CallSign (54-85)
        callsign_bytes = self.callsign.encode('ascii')[:32].ljust(32, b'\x00')
        packet[54:86] = callsign_bytes

        # Reserved (86-89)
        packet[86:90] = self.reserved

        # DATA (90+)
        if self.data:
            packet[DRAARL_HEADER_SIZE:] = self.data

        return bytes(packet)


def encode_packet(
    username: str,
    device_password: str,
    ssid: int,
    packet_type: int,
    dev_model: int = 0,
    dmrid: int = 0,
    callsign: str = "",
    data: bytes = b''
) -> bytes:
    """便捷函数：编码协议包"""
    packet = DraARLv1Packet()
    packet.username = username
    packet.device_password = device_password
    packet.ssid = ssid
    packet.packet_type = packet_type
    packet.dev_model = dev_model
    packet.dmrid = dmrid
    packet.callsign = callsign
    packet.data = data
    return packet.encode()


def parse_merged_opus_frames(data: bytes) -> List[bytes]:
    """
    解析合并的 Opus 帧格式
    格式：[Len1(2B)][Data1][Len2(2B)][Data2]...
    兼容单帧格式（无长度前缀）
    """
    frames = []
    offset = 0

    while offset + 2 <= len(data):
        frame_length = struct.unpack('>H', data[offset:offset+2])[0]

        # 安全检查：帧长度必须合理
        if frame_length == 0 or frame_length > 1000 or offset + 2 + frame_length > len(data):
            if offset == 0:
                return [data]
            break

        frames.append(data[offset+2:offset+2+frame_length])
        offset += 2 + frame_length

    if not frames:
        return [data]

    return frames


def build_merged_opus_frames(frames: List[bytes]) -> bytes:
    """构建合并的 Opus 帧数据"""
    merged = b''
    for frame in frames:
        merged += struct.pack('>H', len(frame))
        merged += frame
    return merged


def is_ghost_dev_model(dev_model: int) -> bool:
    """判断是否为 UDP 幽灵设备型号 (101-104)"""
    return DevModel.ANDROID <= dev_model <= DevModel.MACOS


def is_ghost_dev_model_or_web(dev_model: int) -> bool:
    """判断是否为幽灵设备型号（包括 Web 105）"""
    return DevModel.ANDROID <= dev_model <= DevModel.BROWSER


def get_ghost_ssid(dev_model: int) -> int:
    """获取幽灵设备的 SSID（等于 DevModel）"""
    if is_ghost_dev_model_or_web(dev_model):
        return dev_model
    return 0


def get_dev_model_name(dev_model: int) -> str:
    """获取设备型号名称"""
    names = {
        DevModel.UNKNOWN: "Unknown",
        DevModel.WECHAT_MINI: "WeChat Mini",
        DevModel.ANDROID: "Android",
        DevModel.IOS: "iOS",
        DevModel.WINDOWS: "Windows",
        DevModel.MACOS: "macOS",
        DevModel.BROWSER: "Web Browser",
        DevModel.INTERCONNECT: "Interconnect",
    }
    return names.get(dev_model, f"Unknown({dev_model})")


# ==========================================
# Config 包协议常量 (Type=0x03)
# 仅用于 UDP 普通设备的配置同步
# ==========================================

# Config 包 DATA 区域操作类型
class ConfigType:
    QUERY = 0x01      # 查询配置请求
    SET = 0x02        # 配置下发/上报
    TIME_SYNC = 0x03  # 时间同步

# TLV 配置项 Type 定义
class TLVType:
    RX_FREQ = 0x01      # 接收频率 (8 bytes, big-endian uint64 Hz)
    TX_FREQ = 0x02      # 发射频率 (8 bytes, big-endian uint64 Hz)
    RX_CTCSS = 0x03     # 接收亚音 (4 bytes, big-endian float32 Hz, 0=关闭)
    TX_CTCSS = 0x04     # 发射亚音 (4 bytes, big-endian float32 Hz, 0=关闭)
    SQL_LEVEL = 0x05    # 静噪等级 (1 byte, uint8 0-8)
    POWER_LEVEL = 0x06  # 功率等级 (1 byte, uint8 1=低, 3=高)
    TX_BANDWIDTH = 0x07 # 发射带宽 (1 byte, uint8 1=窄带, 2=宽带)
    RX_TONE_MODE = 0x08   # 接收亚音类型 (1 byte, 0=OFF,1=CTCSS,2=CDCSS_N,3=CDCSS_I)
    RX_TONE_VALUE = 0x09  # 接收亚音值 (8 bytes, ASCII, 如 88.5/023)
    TX_TONE_MODE = 0x0A   # 发射亚音类型 (1 byte, 0=OFF,1=CTCSS,2=CDCSS_N,3=CDCSS_I)
    TX_TONE_VALUE = 0x0B  # 发射亚音值 (8 bytes, ASCII, 如 88.5/023)
    TIMESTAMP = 0x10    # 时间戳 (8 bytes, big-endian int64 Unix毫秒)

# TLV Type 到配置键名的映射
TLV_TYPE_TO_KEY = {
    TLVType.RX_FREQ: "rx_freq",
    TLVType.TX_FREQ: "tx_freq",
    TLVType.RX_CTCSS: "rx_ctcss",
    TLVType.TX_CTCSS: "tx_ctcss",
    TLVType.SQL_LEVEL: "sql_level",
    TLVType.POWER_LEVEL: "power_level",
    TLVType.TX_BANDWIDTH: "tx_bandwidth",
    TLVType.RX_TONE_MODE: "rx_tone_mode",
    TLVType.RX_TONE_VALUE: "rx_tone_value",
    TLVType.TX_TONE_MODE: "tx_tone_mode",
    TLVType.TX_TONE_VALUE: "tx_tone_value",
    TLVType.TIMESTAMP: "timestamp",
}

# 配置键名到 TLV Type 的映射
KEY_TO_TLV_TYPE = {v: k for k, v in TLV_TYPE_TO_KEY.items()}

# TLV 长度定义
TLV_LENGTH = {
    TLVType.RX_FREQ: 8,
    TLVType.TX_FREQ: 8,
    TLVType.RX_CTCSS: 4,
    TLVType.TX_CTCSS: 4,
    TLVType.SQL_LEVEL: 1,
    TLVType.POWER_LEVEL: 1,
    TLVType.TX_BANDWIDTH: 1,
    TLVType.RX_TONE_MODE: 1,
    TLVType.RX_TONE_VALUE: 8,
    TLVType.TX_TONE_MODE: 1,
    TLVType.TX_TONE_VALUE: 8,
    TLVType.TIMESTAMP: 8,
}

TONE_MODE_TO_BYTE = {
    "off": 0,
    "ctcss": 1,
    "cdcss_n": 2,
    "cdcss_i": 3,
}

BYTE_TO_TONE_MODE = {
    0: "off",
    1: "ctcss",
    2: "cdcss_n",
    3: "cdcss_i",
}


def encode_tlv(configs: dict, return_count: bool = False):
    """
    将配置 dict 编码为 TLV 格式
    返回: 完整的 TLV 列表（不含 DATA[0] 和 DATA[1]）
    """
    result = bytearray()
    count = 0
    for key, value in configs.items():
        tlv_type = KEY_TO_TLV_TYPE.get(key)
        if tlv_type is None:
            continue
        length = TLV_LENGTH.get(tlv_type)
        if length is None:
            continue

        # Type (1 byte)
        result.append(tlv_type)
        # Length (1 byte)
        result.append(length)
        # Value (N bytes)
        value_bytes = _encode_tlv_value(tlv_type, str(value))
        if len(value_bytes) != length:
            if len(value_bytes) > length:
                value_bytes = value_bytes[:length]
            else:
                value_bytes = value_bytes.ljust(length, b'\x00')
        result.extend(value_bytes)
        count += 1

    encoded = bytes(result)
    if return_count:
        return encoded, count
    return encoded


def _normalize_tone_mode(value: str) -> str:
    raw = str(value).strip().lower()
    if raw in ("", "off", "0"):
        return "off"
    if raw in ("ctcss", "1"):
        return "ctcss"
    if raw in ("cdcss_n", "cdcss-n", "dcs", "dcs_n", "2"):
        return "cdcss_n"
    if raw in ("cdcss_i", "cdcss-i", "dcs_i", "3"):
        return "cdcss_i"
    return "off"


def _encode_tlv_value(tlv_type: int, value: str) -> bytes:
    """编码单个 TLV 值"""
    if tlv_type in (TLVType.RX_FREQ, TLVType.TX_FREQ):
        # 8 bytes, big-endian uint64
        try:
            freq = int(value)
        except ValueError:
            freq = 0
        return struct.pack('>Q', freq)

    elif tlv_type in (TLVType.RX_CTCSS, TLVType.TX_CTCSS):
        # 4 bytes, big-endian float32
        try:
            ctcss = float(value)
        except ValueError:
            ctcss = 0.0
        return struct.pack('>f', ctcss)

    elif tlv_type in (TLVType.SQL_LEVEL, TLVType.POWER_LEVEL, TLVType.TX_BANDWIDTH):
        # 1 byte, uint8
        try:
            val = int(value)
        except ValueError:
            val = 0
        return bytes([val & 0xFF])

    elif tlv_type in (TLVType.RX_TONE_MODE, TLVType.TX_TONE_MODE):
        mode = _normalize_tone_mode(value)
        return bytes([TONE_MODE_TO_BYTE.get(mode, 0)])

    elif tlv_type in (TLVType.RX_TONE_VALUE, TLVType.TX_TONE_VALUE):
        # 8 bytes, ASCII
        return value.strip().encode('ascii', errors='ignore')[:8].ljust(8, b'\x00')

    elif tlv_type == TLVType.TIMESTAMP:
        # 8 bytes, big-endian int64 (Unix毫秒)
        try:
            ts = int(value)
        except ValueError:
            ts = int(time.time() * 1000)
        return struct.pack('>q', ts)

    return bytes(TLV_LENGTH.get(tlv_type, 0))


def decode_tlv(data: bytes) -> dict:
    """
    将 TLV 格式的 bytes 解码为配置 dict
    输入: DATA[2:] 开始的 TLV 列表（跳过 DATA[0] 和 DATA[1]）
    """
    result = {}
    offset = 0

    while offset < len(data):
        if offset + 2 > len(data):
            break

        tlv_type = data[offset]
        length = data[offset + 1]
        offset += 2

        if offset + length > len(data):
            key = TLV_TYPE_TO_KEY.get(tlv_type)
            if key is not None:
                result[key] = _default_value_for_tlv(tlv_type)
            break

        value_bytes = data[offset:offset + length]
        offset += length

        key = TLV_TYPE_TO_KEY.get(tlv_type)
        if key is None:
            continue

        expected_len = TLV_LENGTH.get(tlv_type)
        if expected_len is not None and expected_len != length:
            result[key] = _default_value_for_tlv(tlv_type)
            continue

        value = _decode_tlv_value(tlv_type, value_bytes)
        result[key] = value

    return result


def _default_value_for_tlv(tlv_type: int) -> str:
    if tlv_type in (TLVType.RX_TONE_MODE, TLVType.TX_TONE_MODE):
        return "off"
    if tlv_type in (TLVType.RX_TONE_VALUE, TLVType.TX_TONE_VALUE):
        return ""
    return "0"


def _decode_tlv_value(tlv_type: int, data: bytes) -> str:
    """解码单个 TLV 值"""
    if tlv_type in (TLVType.RX_FREQ, TLVType.TX_FREQ):
        if len(data) != 8:
            return "0"
        freq = struct.unpack('>Q', data[:8])[0]
        return str(freq)

    elif tlv_type in (TLVType.RX_CTCSS, TLVType.TX_CTCSS):
        if len(data) != 4:
            return "0"
        ctcss = struct.unpack('>f', data[:4])[0]
        return f"{ctcss:.1f}"

    elif tlv_type in (TLVType.SQL_LEVEL, TLVType.POWER_LEVEL, TLVType.TX_BANDWIDTH):
        if len(data) != 1:
            return "0"
        return str(data[0])

    elif tlv_type in (TLVType.RX_TONE_MODE, TLVType.TX_TONE_MODE):
        if len(data) != 1:
            return "off"
        return BYTE_TO_TONE_MODE.get(data[0], "off")

    elif tlv_type in (TLVType.RX_TONE_VALUE, TLVType.TX_TONE_VALUE):
        if len(data) != 8:
            return ""
        return data.decode('ascii', errors='ignore').rstrip('\x00').strip()

    elif tlv_type == TLVType.TIMESTAMP:
        if len(data) != 8:
            return "0"
        ts = struct.unpack('>q', data[:8])[0]
        return str(ts)

    return ""


def build_config_query_packet() -> bytes:
    """构建配置查询包 (DATA[0] = 0x01)"""
    return bytes([ConfigType.QUERY])


def build_config_set_packet(configs: dict) -> bytes:
    """
    构建配置下发/上报包 (DATA[0] = 0x02)
    configs: 要下发/上报的配置项 dict
    """
    if not configs:
        return bytes([ConfigType.SET, 0x00])

    tlv_data, item_count = encode_tlv(configs, return_count=True)
    result = bytearray()
    result.append(ConfigType.SET)
    result.append(item_count)  # 实际编码配置项数量
    result.extend(tlv_data)

    return bytes(result)


def build_time_sync_packet() -> bytes:
    """构建时间同步包 (DATA[0] = 0x03)"""
    result = bytearray()
    result.append(ConfigType.TIME_SYNC)
    result.append(0x00)  # 保留字节
    timestamp = int(time.time() * 1000)
    result.extend(struct.pack('>q', timestamp))
    return bytes(result)


def parse_config_packet(data: bytes) -> dict:
    """
    解析 Config 包
    返回: {"type": "query"/"set"/"time_sync", "configs": {...}, "timestamp": ...}
    """
    if len(data) < 1:
        return {"type": "unknown"}

    packet_type = data[0]

    if packet_type == ConfigType.QUERY:
        return {"type": "query"}

    elif packet_type == ConfigType.SET:
        if len(data) < 2:
            return {"type": "set", "configs": {}}
        # data[1] = 配置项数量
        configs = decode_tlv(data[2:])
        return {"type": "set", "configs": configs}

    elif packet_type == ConfigType.TIME_SYNC:
        if len(data) < 10:
            return {"type": "time_sync", "timestamp": 0}
        timestamp = struct.unpack('>q', data[2:10])[0]
        return {"type": "time_sync", "timestamp": timestamp}

    return {"type": "unknown"}


# 导入 time 模块（用于时间戳）
import time
