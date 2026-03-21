"""
UDP JWT 客户端（使用 JWT Token 认证）
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
    JWTAuthStatus, is_ghost_dev_model, get_ghost_ssid, get_dev_model_name
)


class UDPJWTClient(BaseClient):
    """
    UDP JWT 客户端
    使用 JWT Token 进行认证
    适用于幽灵设备 (DevModel 101-104)
    """

    def __init__(
        self,
        server_ip: str,
        server_port: int,
        jwt_token: str,
        dev_model: int = DevModel.WINDOWS,
        log_callback: Optional[Callable[[str], None]] = None,
        enable_audio: bool = True
    ):
        super().__init__(log_callback)

        self.server_addr = (server_ip, server_port)
        self.jwt_token = jwt_token
        self.dev_model = dev_model
        self.enable_audio = enable_audio and PYAUDIO_AVAILABLE and OPUS_AVAILABLE

        # JWT 认证后 SSID 等于 DevModel
        self.ssid = get_ghost_ssid(dev_model)

        # 验证设备型号
        if not is_ghost_dev_model(dev_model):
            self.log(f"[警告] DevModel {dev_model} 不是有效的 UDP 幽灵设备型号 (应为 101-104)")

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

        self.log(f"[配置] 设备型号: {get_dev_model_name(dev_model)}, SSID: {self.ssid}")

    def _init_audio(self):
        """初始化音频引擎"""
        self.pyaudio_inst = pyaudio.PyAudio()
        self.audio_format = pyaudio.paInt16
        self.channels = 1
        self.rate = 16000

        self.frame_duration_ms = 60
        self.chunk_size = int(self.rate * self.frame_duration_ms / 1000)
        self.frames_per_packet = 2
        self.frame_accumulator = []

        self.opus_encoder = opuslib.Encoder(self.rate, self.channels, opuslib.APPLICATION_VOIP)
        self.opus_decoder = opuslib.Decoder(self.rate, self.channels)

        self.log(f"[音频] Opus 16kHz, 60ms帧, 2帧合并")

    def connect(self) -> bool:
        """连接并进行 JWT 认证"""
        try:
            self._set_state(ClientState.CONNECTING)

            # 创建 UDP socket
            self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            self.sock.settimeout(3.0)

            self.running = True

            # 发送 JWT 认证包
            self._set_state(ClientState.AUTHENTICATING)
            self._send_jwt_auth()

            # 等待认证响应
            start_time = time.time()
            while not self.authenticated and time.time() - start_time < 5:
                try:
                    data, addr = self.sock.recvfrom(4096)
                    if self._handle_auth_response(data):
                        break
                except socket.timeout:
                    continue

            if not self.authenticated:
                self.log("[认证失败] 超时未收到响应")
                self._set_state(ClientState.ERROR)
                return False

            # 设置非阻塞
            self.sock.settimeout(1.0)

            # 启动接收线程
            self._start_thread(self._receive_loop, "UDP-JWT-Receiver")

            # 启动心跳线程
            self._start_thread(self._heartbeat_loop, "UDP-JWT-Heartbeat")

            # 启动音频采集线程
            if self.enable_audio:
                self._start_thread(self._transmit_loop, "UDP-JWT-Transmit")

            return True

        except Exception as e:
            self.log(f"[连接错误] {e}")
            self._set_state(ClientState.ERROR)
            return False

    def _send_jwt_auth(self):
        """发送 JWT 认证包"""
        if not self.sock:
            return

        # JWT 认证包：Type=1，DATA 区域放 Token
        packet = encode_packet(
            username="",  # 用户名从 Token 解析
            device_password="",
            ssid=0,  # SSID 由服务器根据 DevModel 分配
            packet_type=PacketType.JWT_AUTH,
            dev_model=self.dev_model,
            dmrid=0,
            data=self.jwt_token.encode('utf-8')
        )

        self.sock.sendto(packet, self.server_addr)
        self.log("[JWT认证] 已发送认证请求...")

    def _handle_auth_response(self, data: bytes) -> bool:
        """处理认证响应"""
        if len(data) < 91:
            return False

        packet = DraARLv1Packet.decode(data)
        if not packet or packet.packet_type != PacketType.JWT_AUTH:
            return False

        status = packet.data[0] if packet.data else JWTAuthStatus.INVALID_TOKEN

        if status == JWTAuthStatus.SUCCESS:
            self.callsign = packet.callsign
            self.ssid = packet.ssid
            self.authenticated = True
            self._set_state(ClientState.CONNECTED)
            self.log(f"[认证成功] 呼号: {self.callsign}, SSID: {self.ssid}")
            return True
        else:
            error_msgs = {
                JWTAuthStatus.INVALID_TOKEN: "Token 无效或过期",
                JWTAuthStatus.USER_NOT_FOUND: "用户不存在",
                JWTAuthStatus.USER_DISABLED: "用户已禁用",
                JWTAuthStatus.USER_NOT_APPROVED: "用户未审核",
                JWTAuthStatus.INVALID_DEV_MODEL: "无效的设备型号",
            }
            error_msg = error_msgs.get(status, "未知错误")
            extra_msg = packet.data[1:].decode('utf-8', errors='replace') if len(packet.data) > 1 else ""
            self.log(f"[认证失败] {error_msg}: {extra_msg}")
            self._set_state(ClientState.ERROR)
            return True

        return False

    def disconnect(self):
        """断开连接"""
        self.running = False
        self._set_state(ClientState.DISCONNECTED)

        if self.sock:
            self.sock.close()
            self.sock = None

        if self.pyaudio_inst:
            self.pyaudio_inst.terminate()

    def _send_heartbeat(self):
        """发送心跳包"""
        if not self.sock:
            return

        # GPS 数据
        gps_data = struct.pack('>ddd', self.gps_lat, self.gps_lon, self.gps_alt)

        packet = encode_packet(
            username="",  # JWT 认证后服务器已知道用户
            device_password="",
            ssid=self.ssid,
            packet_type=PacketType.HEARTBEAT,
            dev_model=self.dev_model,
            dmrid=0,
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

                # 语音数据
                if packet.packet_type == PacketType.OPUS_16K and packet.data:
                    if self.is_transmitting:
                        continue

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

                elif packet.packet_type in [PacketType.CONTROL, PacketType.HEARTBEAT]:
                    last_sender = None

            except socket.timeout:
                continue
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
            username="",
            device_password="",
            ssid=self.ssid,
            packet_type=PacketType.OPUS_16K,
            dev_model=self.dev_model,
            dmrid=0,
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
            username="",
            device_password="",
            ssid=self.ssid,
            packet_type=PacketType.TEXT_MESSAGE,
            dev_model=self.dev_model,
            dmrid=0,
            data=text.encode('utf-8')
        )

        self.sock.sendto(packet, self.server_addr)
        self.log(f"[文字发出] {text}")

    def set_gps(self, lat: float, lon: float, alt: float = 0.0):
        """设置 GPS 位置"""
        self.gps_lat = lat
        self.gps_lon = lon
        self.gps_alt = alt
