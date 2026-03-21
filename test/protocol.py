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
