"""
WebSocket JWT 客户端（使用 JWT Token 认证）
"""

import struct
import time
import threading
from typing import Callable, Optional

try:
    import websocket
    WEBSOCKET_AVAILABLE = True
except ImportError:
    WEBSOCKET_AVAILABLE = False

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
    parse_merged_opus_frames, build_merged_opus_frames
)


class WSJWTClient(BaseClient):
    """
    WebSocket JWT 客户端
    使用 JWT Token 进行认证
    适用于 Web 浏览器端 (DevModel=105, SSID=105)
    """

    def __init__(
        self,
        server_url: str,
        jwt_token: str,
        log_callback: Optional[Callable[[str], None]] = None,
        enable_audio: bool = True
    ):
        super().__init__(log_callback)

        self.server_url = server_url
        self.jwt_token = jwt_token
        self.enable_audio = enable_audio and PYAUDIO_AVAILABLE and OPUS_AVAILABLE

        # JWT 认证后固定 SSID=105
        self.ssid = 105
        self.dev_model = DevModel.BROWSER

        # WebSocket
        self.ws = None
        self.ws_lock = threading.Lock()

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

    def _build_ws_url(self) -> str:
        """构建带 Token 的 WebSocket URL"""
        # Token 通过 URL 参数传递
        separator = '&' if '?' in self.server_url else '?'
        return f"{self.server_url}{separator}token={self.jwt_token}"

    def connect(self) -> bool:
        """连接服务器"""
        if not WEBSOCKET_AVAILABLE:
            self.log("[错误] 未安装 websocket-client 库")
            self._set_state(ClientState.ERROR)
            return False

        try:
            self._set_state(ClientState.CONNECTING)

            # 构建带 Token 的 URL
            ws_url = self._build_ws_url()
            self.log(f"[连接] {ws_url[:60]}...")

            # 创建 WebSocket 连接
            self.ws = websocket.create_connection(ws_url)
            self.running = True

            # JWT 认证通过 URL 参数完成，连接成功即认证成功
            self._set_state(ClientState.AUTHENTICATING)

            # 等待认证确认（服务器会发送心跳响应）
            start_time = time.time()
            while not self.authenticated and time.time() - start_time < 5:
                try:
                    data = self.ws.recv()
                    if self._handle_auth_response(data):
                        break
                except Exception as e:
                    self.log(f"[认证等待] {e}")
                    break

            if not self.authenticated:
                # 即使没有收到明确响应，也认为连接成功
                self.authenticated = True
                self._set_state(ClientState.CONNECTED)
                self.log("[连接成功] (无显式认证响应)")

            # 启动接收线程
            self._start_thread(self._receive_loop, "WS-JWT-Receiver")

            # 启动心跳线程
            self._start_thread(self._heartbeat_loop, "WS-JWT-Heartbeat")

            # 启动音频采集线程
            if self.enable_audio:
                self._start_thread(self._transmit_loop, "WS-JWT-Transmit")

            return True

        except Exception as e:
            self.log(f"[连接错误] {e}")
            self._set_state(ClientState.ERROR)
            return False

    def _handle_auth_response(self, data) -> bool:
        """处理认证响应"""
        if isinstance(data, str):
            return False

        if len(data) < 90:
            return False

        packet = DraARLv1Packet.decode(data)
        if not packet:
            return False

        # 心跳响应包含呼号
        if packet.packet_type == PacketType.HEARTBEAT and packet.callsign:
            self.callsign = packet.callsign
            self.ssid = packet.ssid or self.ssid
            self.authenticated = True
            self._set_state(ClientState.CONNECTED)
            self.log(f"[认证成功] 呼号: {self.callsign}, SSID: {self.ssid}")
            return True

        return False

    def disconnect(self):
        """断开连接"""
        self.running = False
        self._set_state(ClientState.DISCONNECTED)

        with self.ws_lock:
            if self.ws:
                try:
                    self.ws.close()
                except:
                    pass
                self.ws = None

        if self.pyaudio_inst:
            try:
                self.pyaudio_inst.terminate()
            except:
                pass

    def _send_heartbeat(self):
        """发送心跳包"""
        with self.ws_lock:
            if not self.ws:
                return

            # GPS 数据
            gps_data = struct.pack('>ddd', self.gps_lat, self.gps_lon, self.gps_alt)

            packet = encode_packet(
                username="",
                device_password="",
                ssid=self.ssid,
                packet_type=PacketType.HEARTBEAT,
                dev_model=self.dev_model,
                dmrid=0,
                data=gps_data
            )

            try:
                self.ws.send_binary(packet)
            except Exception as e:
                self.log(f"[心跳发送错误] {e}")

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
                with self.ws_lock:
                    if not self.ws:
                        break
                    data = self.ws.recv()

                if isinstance(data, str):
                    continue

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

            except Exception as e:
                if self.running:
                    self.log(f"[接收错误] {e}")
                    break

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

            except Exception:
                pass  # 静默处理采集错误

        stream_in.stop_stream()
        stream_in.close()

    def _send_merged_audio(self):
        """发送合并的音频帧"""
        with self.ws_lock:
            if not self.ws or not self.frame_accumulator:
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

            try:
                self.ws.send_binary(packet)
            except Exception as e:
                self.log(f"[音频发送错误] {e}")

    def send_heartbeat(self):
        """手动发送心跳"""
        self._send_heartbeat()

    def send_text(self, text: str):
        """发送文本消息"""
        with self.ws_lock:
            if not self.ws or not self.authenticated:
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

            try:
                self.ws.send_binary(packet)
                self.log(f"[文字发出] {text}")
            except Exception as e:
                self.log(f"[文字发送错误] {e}")

    def set_gps(self, lat: float, lon: float, alt: float = 0.0):
        """设置 GPS 位置"""
        self.gps_lat = lat
        self.gps_lon = lon
        self.gps_alt = alt
