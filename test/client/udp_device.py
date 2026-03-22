"""
UDP 普通设备客户端（使用设备密码认证）
"""

import socket
import struct
import time
import threading
from typing import Callable, Optional

try:
    import pyaudio
    PYAUDIO_AVAILABLE = True
except ImportError:
    PYAUDIO_AVAILABLE = False

try:
    import opuslib
    OPUS_AVAILABLE = True
except ImportError:
    OPUS_AVAILABLE = False

from .base import BaseClient, ClientState
from protocol import (
    PacketType, DevModel, encode_packet, DraARLv1Packet,
    parse_merged_opus_frames, build_merged_opus_frames,
    ConfigType, TLVType, KEY_TO_TLV_TYPE, encode_tlv, decode_tlv,
    build_config_set_packet, build_config_query_packet, parse_config_packet
)


class UDPDeviceClient(BaseClient):
    """
    UDP 普通设备客户端
    使用设备密码进行认证（心跳包）
    """

    def __init__(
        self,
        server_ip: str,
        server_port: int,
        username: str,
        device_password: str,
        ssid: int,
        dmrid: int = 0,
        dev_model: int = DevModel.WINDOWS,
        log_callback: Optional[Callable[[str], None]] = None,
        enable_audio: bool = True
    ):
        super().__init__(log_callback)

        self.server_addr = (server_ip, server_port)
        self.username = username
        self.device_password = device_password
        self.ssid = ssid
        self.dmrid = dmrid
        self.dev_model = dev_model
        self.enable_audio = enable_audio and PYAUDIO_AVAILABLE and OPUS_AVAILABLE

        # 网络
        self.sock: Optional[socket.socket] = None

        # GPS
        self.gps_lat = 0.0
        self.gps_lon = 0.0
        self.gps_alt = 0.0

        # 音频引擎
        if self.enable_audio:
            self._init_audio()
        else:
            self.pyaudio_inst = None
            self.opus_encoder = None
            self.opus_decoder = None

        # 设备配置（模拟设备参数）
        self.device_config = {
            TLVType.RX_FREQ: "439500000",      # 接收频率 Hz
            TLVType.TX_FREQ: "439500000",      # 发射频率 Hz
            TLVType.RX_CTCSS: "0",             # 接收亚音
            TLVType.TX_CTCSS: "0",             # 发射亚音
            TLVType.SQL_LEVEL: "3",            # 静噪等级
            TLVType.POWER_LEVEL: "3",          # 功率等级（高）
            TLVType.TX_BANDWIDTH: "2",         # 发射带宽（宽带）
        }

        # 配置更新回调
        self.config_update_callback: Optional[Callable[[dict], None]] = None

    def _init_audio(self):
        """初始化音频引擎"""
        self.pyaudio_inst = pyaudio.PyAudio()
        self.audio_format = pyaudio.paInt16
        self.channels = 1
        self.rate = 16000

        # 60ms 帧时长 + 2 帧合并发送
        self.frame_duration_ms = 60
        self.chunk_size = int(self.rate * self.frame_duration_ms / 1000)  # 960 samples
        self.frames_per_packet = 2
        self.frame_accumulator = []

        self.opus_encoder = opuslib.Encoder(self.rate, self.channels, opuslib.APPLICATION_VOIP)
        self.opus_decoder = opuslib.Decoder(self.rate, self.channels)

        self.log(f"[音频] Opus 16kHz, 60ms帧, 2帧合并")

    def connect(self) -> bool:
        """连接服务器"""
        try:
            self._set_state(ClientState.CONNECTING)

            # 创建 UDP socket
            self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            self.sock.settimeout(1.0)

            self.running = True

            # 启动接收线程
            self._start_thread(self._receive_loop, "UDP-Receiver")

            # 启动心跳线程
            self._start_thread(self._heartbeat_loop, "UDP-Heartbeat")

            # 启动音频采集线程
            if self.enable_audio:
                self._start_thread(self._transmit_loop, "UDP-Transmit")

            # 发送首次心跳进行认证
            self._set_state(ClientState.AUTHENTICATING)
            self._send_heartbeat()

            # 等待认证结果
            time.sleep(0.5)

            return True

        except Exception as e:
            self.log(f"[连接错误] {e}")
            self._set_state(ClientState.ERROR)
            return False

    def disconnect(self):
        """断开连接"""
        if not self.running:
            return  # 防止重复断开

        self.running = False
        self.authenticated = False
        self._set_state(ClientState.DISCONNECTED)

        # 先关闭 socket，解除 recvfrom 阻塞
        if self.sock:
            try:
                self.sock.close()
            except:
                pass
            self.sock = None

        # 等待线程结束（最多等待 2 秒）
        for t in self._threads[:]:
            if t.is_alive():
                t.join(timeout=0.5)
        self._threads.clear()

        # 最后关闭音频
        if self.pyaudio_inst:
            try:
                self.pyaudio_inst.terminate()
            except:
                pass
            self.pyaudio_inst = None

    def _send_heartbeat(self):
        """发送心跳包"""
        if not self.sock:
            return

        # GPS 数据 (24 字节)
        gps_data = struct.pack('>ddd', self.gps_lat, self.gps_lon, self.gps_alt)

        packet = encode_packet(
            username=self.username,
            device_password=self.device_password,
            ssid=self.ssid,
            packet_type=PacketType.HEARTBEAT,
            dev_model=self.dev_model,
            dmrid=self.dmrid,
            data=gps_data
        )

        self.sock.sendto(packet, self.server_addr)

    def _heartbeat_loop(self):
        """心跳循环"""
        while self.running:
            self._send_heartbeat()
            time.sleep(2)

    def _receive_loop(self):
        """接收循环"""
        stream_out = None
        if self.enable_audio and self.pyaudio_inst:
            stream_out = self.pyaudio_inst.open(
                format=self.audio_format,
                channels=self.channels,
                rate=self.rate,
                output=True
            )

        last_sender = None

        while self.running:
            try:
                data, addr = self.sock.recvfrom(4096)
                if len(data) < 90:
                    continue

                packet = DraARLv1Packet.decode(data)
                if not packet:
                    continue

                # 认证成功：收到呼号
                if packet.callsign and not self.authenticated:
                    self.callsign = packet.callsign
                    self.authenticated = True
                    self._set_state(ClientState.CONNECTED)
                    self.log(f"[认证成功] 呼号: {packet.callsign}")

                # 语音数据
                if packet.packet_type == PacketType.OPUS_16K and packet.data:
                    if self.is_transmitting:
                        continue  # 半双工防回音

                    sender_info = f"{packet.callsign}-{packet.ssid}" if packet.callsign else f"SSID-{packet.ssid}"
                    if last_sender != sender_info:
                        self.log(f"[接收] {sender_info} 正在发言...")
                        last_sender = sender_info

                    if stream_out and self.opus_decoder:
                        try:
                            frames = parse_merged_opus_frames(packet.data)
                            for frame in frames:
                                pcm = self.opus_decoder.decode(frame, self.chunk_size)
                                stream_out.write(pcm)
                        except Exception as e:
                            self.log(f"[音频解码错误] {e}")

                # 文本消息
                elif packet.packet_type == PacketType.TEXT_MESSAGE and packet.data:
                    msg = packet.data.decode('utf-8', errors='replace')
                    sender = f"{packet.callsign}-{packet.ssid}" if packet.callsign else f"SSID-{packet.ssid}"
                    self.log(f"[文字] {sender}: {msg}")

                # 重置发言者
                elif packet.packet_type in [PacketType.CONTROL, PacketType.HEARTBEAT]:
                    last_sender = None

                # Config 配置包
                elif packet.packet_type == PacketType.CONFIG and packet.data:
                    self._handle_config_packet(packet.data)

            except socket.timeout:
                continue
            except OSError:
                # socket 被关闭时会发生此错误
                break
            except Exception as e:
                if self.running:
                    self.log(f"[接收错误] {e}")

        if stream_out:
            stream_out.stop_stream()
            stream_out.close()

    def _transmit_loop(self):
        """音频采集和发送循环"""
        if not self.pyaudio_inst:
            return

        try:
            stream_in = self.pyaudio_inst.open(
                format=self.audio_format,
                channels=self.channels,
                rate=self.rate,
                input=True,
                frames_per_buffer=self.chunk_size
            )
            self.log("[麦克风] 已就绪")
        except Exception as e:
            self.log(f"[麦克风错误] {e}")
            return

        while self.running:
            try:
                pcm_data = stream_in.read(self.chunk_size, exception_on_overflow=False)

                if self.is_transmitting and self.opus_encoder:
                    encoded = self.opus_encoder.encode(pcm_data, self.chunk_size)
                    self.frame_accumulator.append(encoded)

                    if len(self.frame_accumulator) >= self.frames_per_packet:
                        self._send_merged_audio()

            except Exception as e:
                if self.running:
                    self.log(f"[音频采集错误] {e}")

        stream_in.stop_stream()
        stream_in.close()

    def _send_merged_audio(self):
        """发送合并的音频帧"""
        if not self.sock or not self.frame_accumulator:
            return

        frames = self.frame_accumulator[:self.frames_per_packet]
        self.frame_accumulator = self.frame_accumulator[self.frames_per_packet:]

        merged = build_merged_opus_frames(frames)

        packet = encode_packet(
            username=self.username,
            device_password=self.device_password,
            ssid=self.ssid,
            packet_type=PacketType.OPUS_16K,
            dev_model=self.dev_model,
            dmrid=self.dmrid,
            data=merged
        )

        self.sock.sendto(packet, self.server_addr)

    def send_heartbeat(self):
        """手动发送心跳"""
        self._send_heartbeat()

    def send_text(self, text: str):
        """发送文本消息"""
        if not self.sock or not self.authenticated:
            self.log("[错误] 未连接")
            return

        packet = encode_packet(
            username=self.username,
            device_password=self.device_password,
            ssid=self.ssid,
            packet_type=PacketType.TEXT_MESSAGE,
            dev_model=self.dev_model,
            dmrid=self.dmrid,
            data=text.encode('utf-8')
        )

        self.sock.sendto(packet, self.server_addr)
        self.log(f"[文字发出] {text}")

    def set_gps(self, lat: float, lon: float, alt: float = 0.0):
        """设置 GPS 位置"""
        self.gps_lat = lat
        self.gps_lon = lon
        self.gps_alt = alt

    # ============================================================
    # Config 配置包处理
    # ============================================================

    def _handle_config_packet(self, data: bytes):
        """处理收到的 Config 包"""
        try:
            result = parse_config_packet(data)
            config_type = result.get("type")

            if config_type == "query":
                # 服务器查询配置，回复当前配置
                self.log(f"[Config] 收到配置查询，回复当前配置")
                self._send_config_report()

            elif config_type == "set":
                # 服务器下发配置，更新本地配置
                configs = result.get("configs", {})
                self.log(f"[Config] 收到配置下发: {configs}")
                for key, value in configs.items():
                    tlv_type = KEY_TO_TLV_TYPE.get(key)
                    if tlv_type and tlv_type in self.device_config:
                        self.device_config[tlv_type] = value
                self.log(f"[Config] 配置已更新")
                # 触发配置更新回调
                if self.config_update_callback:
                    self.config_update_callback(self.get_device_config())

            elif config_type == "time_sync":
                # 时间同步（ACK）- 解析时间戳
                timestamp = result.get("timestamp", 0)
                if timestamp:
                    from datetime import datetime
                    dt = datetime.fromtimestamp(timestamp / 1000)
                    self.log(f"[Config] 收到时间同步(ACK): {dt.strftime('%Y-%m-%d %H:%M:%S')}")

        except Exception as e:
            self.log(f"[Config] 解析错误: {e}")

    def _send_config_report(self):
        """发送配置上报包（响应查询）"""
        if not self.sock:
            return

        # 将 device_config (TLVType -> value) 转换为字符串 key 格式
        config_for_packet = self.get_device_config()

        packet = encode_packet(
            username=self.username,
            device_password=self.device_password,
            ssid=self.ssid,
            packet_type=PacketType.CONFIG,
            dev_model=self.dev_model,
            dmrid=self.dmrid,
            data=build_config_set_packet(config_for_packet)
        )

        self.sock.sendto(packet, self.server_addr)
        self.log(f"[Config] 已上报配置")

    def send_config_query(self):
        """发送配置查询请求"""
        if not self.sock or not self.authenticated:
            self.log("[错误] 未连接")
            return

        packet = encode_packet(
            username=self.username,
            device_password=self.device_password,
            ssid=self.ssid,
            packet_type=PacketType.CONFIG,
            dev_model=self.dev_model,
            dmrid=self.dmrid,
            data=build_config_query_packet()
        )

        self.sock.sendto(packet, self.server_addr)
        self.log("[Config] 已发送配置查询")

    def get_device_config(self) -> dict:
        """获取当前设备配置（人类可读格式）"""
        result = {}
        for tlv_type, value in self.device_config.items():
            name = {
                TLVType.RX_FREQ: "rx_freq",
                TLVType.TX_FREQ: "tx_freq",
                TLVType.RX_CTCSS: "rx_ctcss",
                TLVType.TX_CTCSS: "tx_ctcss",
                TLVType.SQL_LEVEL: "sql_level",
                TLVType.POWER_LEVEL: "power_level",
                TLVType.TX_BANDWIDTH: "tx_bandwidth",
            }.get(tlv_type, str(tlv_type))
            result[name] = value
        return result

    def _get_tlv_type(self, key: str) -> int:
        """根据配置键名获取 TLV Type"""
        mapping = {
            "rx_freq": TLVType.RX_FREQ,
            "tx_freq": TLVType.TX_FREQ,
            "rx_ctcss": TLVType.RX_CTCSS,
            "tx_ctcss": TLVType.TX_CTCSS,
            "sql_level": TLVType.SQL_LEVEL,
            "power_level": TLVType.POWER_LEVEL,
            "tx_bandwidth": TLVType.TX_BANDWIDTH,
        }
        return mapping.get(key, 0)
