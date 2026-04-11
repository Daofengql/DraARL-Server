"""
串口客户端（通过 COM 口直接发送数据包）
用于与 4G 透传模块通信
"""

import serial
import serial.tools.list_ports
import struct
import time
import threading
from typing import Callable, Optional, List

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


class SerialClient(BaseClient):
    """
    串口客户端
    通过串口直接发送 DraARLv1 协议包
    """

    def __init__(
        self,
        port: str,
        baudrate: int = 921600,
        username: str = "",
        device_password: str = "",
        ssid: int = 1,
        dmrid: int = 0,
        dev_model: int = DevModel.WINDOWS,
        log_callback: Optional[Callable[[str], None]] = None,
        enable_audio: bool = True
    ):
        super().__init__(log_callback)

        self.port = port
        self.baudrate = baudrate
        self.username = username
        self.device_password = device_password
        self.ssid = ssid
        self.dmrid = dmrid
        self.dev_model = dev_model
        self.enable_audio = enable_audio and PYAUDIO_AVAILABLE and OPUS_AVAILABLE

        # 调试日志
        self.log(f"[调试] pyaudio可用: {PYAUDIO_AVAILABLE}, opuslib可用: {OPUS_AVAILABLE}")
        self.log(f"[调试] enable_audio参数: {enable_audio}, 最终enable_audio: {self.enable_audio}")

        # 串口
        self.serial_conn: Optional[serial.Serial] = None

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
            if not PYAUDIO_AVAILABLE:
                self.log("[音频] pyaudio 未安装，音频功能禁用")
            elif not OPUS_AVAILABLE:
                self.log("[音频] opuslib 未安装，音频功能禁用")

        # 设备配置
        self.device_config = {
            TLVType.RX_FREQ: "439500000",
            TLVType.TX_FREQ: "439500000",
            TLVType.RX_CTCSS: "0",
            TLVType.TX_CTCSS: "0",
            TLVType.SQL_LEVEL: "3",
            TLVType.POWER_LEVEL: "3",
            TLVType.TX_BANDWIDTH: "2",
            TLVType.RX_TONE_MODE: "off",
            TLVType.RX_TONE_VALUE: "0",
            TLVType.TX_TONE_MODE: "off",
            TLVType.TX_TONE_VALUE: "0",
            TLVType.RF_GUARD_ENABLED: "1",
            TLVType.RF_GUARD_SINGLE_TX_LIMIT_S: "30",
            TLVType.RF_GUARD_WINDOW_S: "300",
            TLVType.RF_GUARD_MAX_TX_IN_WINDOW_S: "60",
        }

        # 配置更新回调
        self.config_update_callback: Optional[Callable[[dict], None]] = None

        # 接收缓冲区
        self.recv_buffer = bytearray()

    @staticmethod
    def list_ports() -> List[str]:
        """列出可用串口"""
        ports = serial.tools.list_ports.comports()
        return [p.device for p in ports]

    def _init_audio(self):
        """初始化音频引擎"""
        self.pyaudio_inst = pyaudio.PyAudio()
        self.audio_format = pyaudio.paInt16
        self.channels = 1
        self.rate = 16000

        self.frame_duration_ms = 60
        self.chunk_size = int(self.rate * self.frame_duration_ms / 1000)
        self.frames_per_packet = 4  # 串口使用4帧合一发送
        self.frame_accumulator = []

        self.opus_encoder = opuslib.Encoder(self.rate, self.channels, opuslib.APPLICATION_VOIP)
        self.opus_decoder = opuslib.Decoder(self.rate, self.channels)

        self.log(f"[音频] Opus 16kHz, 60ms帧, 4帧合一")

    def start_transmit(self):
        """开始发射（PTT按下）"""
        if not self.running:
            self.log("[警告] 未连接，无法发射")
            return
        self.is_transmitting = True
        self.log(">>> [开始发射]")

    def stop_transmit(self):
        """停止发射（PTT释放）"""
        self.is_transmitting = False
        # 清空音频帧累积器
        if hasattr(self, 'frame_accumulator'):
            self.frame_accumulator = []
        self.log("<<< [停止发射]")

    def connect(self) -> bool:
        """连接串口"""
        try:
            self._set_state(ClientState.CONNECTING)

            # 打开串口
            self.serial_conn = serial.Serial(
                port=self.port,
                baudrate=self.baudrate,
                bytesize=serial.EIGHTBITS,
                parity=serial.PARITY_NONE,
                stopbits=serial.STOPBITS_ONE,
                timeout=1.0
            )

            self.log(f"[串口] 已打开 {self.port} @ {self.baudrate}")

            self.running = True

            # 启动接收线程
            self._start_thread(self._receive_loop, "Serial-Receiver")

            # 启动心跳线程
            self._start_thread(self._heartbeat_loop, "Serial-Heartbeat")

            # 启动音频采集线程
            if self.enable_audio:
                self._start_thread(self._transmit_loop, "Serial-Transmit")

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
            return

        self.running = False
        self.authenticated = False
        self._set_state(ClientState.DISCONNECTED)

        # 关闭串口
        if self.serial_conn:
            try:
                self.serial_conn.close()
            except:
                pass
            self.serial_conn = None

        # 等待线程结束
        for t in self._threads[:]:
            if t.is_alive():
                t.join(timeout=0.5)
        self._threads.clear()

        # 关闭音频
        if self.pyaudio_inst:
            try:
                self.pyaudio_inst.terminate()
            except:
                pass
            self.pyaudio_inst = None

    def _send_heartbeat(self):
        """发送心跳包"""
        if not self.serial_conn or not self.serial_conn.is_open:
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

        try:
            self.serial_conn.write(packet)
            self.serial_conn.flush()
        except Exception as e:
            self.log(f"[发送错误] {e}")

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
                # 读取串口数据
                if self.serial_conn and self.serial_conn.is_open:
                    data = self.serial_conn.read(4096)
                    if not data:
                        continue

                    # 添加到缓冲区
                    self.recv_buffer.extend(data)

                    # 尝试解析完整包
                    self._process_buffer(stream_out, last_sender)

            except serial.SerialException as e:
                if self.running:
                    self.log(f"[串口错误] {e}")
                break
            except Exception as e:
                if self.running:
                    self.log(f"[接收错误] {e}")

        if stream_out:
            stream_out.stop_stream()
            stream_out.close()

    def _process_buffer(self, stream_out, last_sender):
        """处理接收缓冲区"""
        while len(self.recv_buffer) >= 90:
            # 查找包头 "DraA"
            header_pos = -1
            for i in range(len(self.recv_buffer) - 3):
                if self.recv_buffer[i:i+4] == b'DraA':
                    header_pos = i
                    break

            if header_pos < 0:
                # 没找到包头，清空缓冲区（保留最后3字节防止截断）
                if len(self.recv_buffer) > 3:
                    self.recv_buffer = self.recv_buffer[-3:]
                return

            # 丢弃包头之前的数据
            if header_pos > 0:
                self.recv_buffer = self.recv_buffer[header_pos:]

            # 检查是否有足够数据读取长度
            if len(self.recv_buffer) < 6:
                return

            # 读取包长度
            packet_len = struct.unpack('>H', self.recv_buffer[4:6])[0]

            # 检查是否收到完整包
            if len(self.recv_buffer) < packet_len:
                return

            # 提取完整包
            raw_packet = bytes(self.recv_buffer[:packet_len])
            self.recv_buffer = self.recv_buffer[packet_len:]

            # 解析包
            packet = DraARLv1Packet.decode(raw_packet)
            if not packet:
                continue

            # 认证成功
            if packet.callsign and not self.authenticated:
                self.callsign = packet.callsign
                self.authenticated = True
                self._set_state(ClientState.CONNECTED)
                self.log(f"[认证成功] 呼号: {packet.callsign}")

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

            # 重置发言者
            elif packet.packet_type in [PacketType.CONTROL, PacketType.HEARTBEAT]:
                last_sender = None

            # Config 配置包
            elif packet.packet_type == PacketType.CONFIG and packet.data:
                self._handle_config_packet(packet.data)

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
        if not self.serial_conn or not self.serial_conn.is_open:
            self.log("[发送] 串口未打开")
            return
        if not self.frame_accumulator:
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

        try:
            self.serial_conn.write(packet)
            self.serial_conn.flush()
            self.log(f"[音频发送] {len(packet)} 字节")
        except Exception as e:
            self.log(f"[发送错误] {e}")

    def send_heartbeat(self):
        """手动发送心跳"""
        self._send_heartbeat()

    def send_text(self, text: str):
        """发送文本消息"""
        if not self.serial_conn or not self.serial_conn.is_open or not self.authenticated:
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

        try:
            self.serial_conn.write(packet)
            self.serial_conn.flush()
            self.log(f"[文字发出] {text}")
        except Exception as e:
            self.log(f"[发送错误] {e}")

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
                self.log(f"[Config] 收到配置查询，回复当前配置")
                self._send_config_report()

            elif config_type == "set":
                configs = result.get("configs", {})
                self.log(f"[Config] 收到配置下发: {configs}")
                for key, value in configs.items():
                    tlv_type = KEY_TO_TLV_TYPE.get(key)
                    if tlv_type and tlv_type in self.device_config:
                        self.device_config[tlv_type] = value
                self.log(f"[Config] 配置已更新")
                if self.config_update_callback:
                    self.config_update_callback(self.get_device_config())

            elif config_type == "time_sync":
                timestamp = result.get("timestamp", 0)
                if timestamp:
                    from datetime import datetime
                    dt = datetime.fromtimestamp(timestamp / 1000)
                    self.log(f"[Config] 收到时间同步(ACK): {dt.strftime('%Y-%m-%d %H:%M:%S')}")

        except Exception as e:
            self.log(f"[Config] 解析错误: {e}")

    def _send_config_report(self):
        """发送配置上报包"""
        if not self.serial_conn or not self.serial_conn.is_open:
            return

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

        try:
            self.serial_conn.write(packet)
            self.serial_conn.flush()
            self.log(f"[Config] 已上报配置")
        except Exception as e:
            self.log(f"[Config] 发送错误: {e}")

    def send_config_query(self):
        """发送配置查询请求"""
        if not self.serial_conn or not self.serial_conn.is_open or not self.authenticated:
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

        try:
            self.serial_conn.write(packet)
            self.serial_conn.flush()
            self.log("[Config] 已发送配置查询")
        except Exception as e:
            self.log(f"[Config] 发送错误: {e}")

    def get_device_config(self) -> dict:
        """获取当前设备配置"""
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
                TLVType.RX_TONE_MODE: "rx_tone_mode",
                TLVType.RX_TONE_VALUE: "rx_tone_value",
                TLVType.TX_TONE_MODE: "tx_tone_mode",
                TLVType.TX_TONE_VALUE: "tx_tone_value",
                TLVType.RF_GUARD_ENABLED: "rf_guard_enabled",
                TLVType.RF_GUARD_SINGLE_TX_LIMIT_S: "rf_guard_single_tx_limit_s",
                TLVType.RF_GUARD_WINDOW_S: "rf_guard_window_s",
                TLVType.RF_GUARD_MAX_TX_IN_WINDOW_S: "rf_guard_max_tx_in_window_s",
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
            "rx_tone_mode": TLVType.RX_TONE_MODE,
            "rx_tone_value": TLVType.RX_TONE_VALUE,
            "tx_tone_mode": TLVType.TX_TONE_MODE,
            "tx_tone_value": TLVType.TX_TONE_VALUE,
            "rf_guard_enabled": TLVType.RF_GUARD_ENABLED,
            "rf_guard_single_tx_limit_s": TLVType.RF_GUARD_SINGLE_TX_LIMIT_S,
            "rf_guard_window_s": TLVType.RF_GUARD_WINDOW_S,
            "rf_guard_max_tx_in_window_s": TLVType.RF_GUARD_MAX_TX_IN_WINDOW_S,
        }
        return mapping.get(key, 0)
