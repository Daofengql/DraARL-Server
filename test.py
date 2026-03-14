import socket
import struct
import threading
import time
import pyaudio
import tkinter as tk
from tkinter import ttk, scrolledtext, messagebox

# 尝试导入 opuslib，用于支持 Type 5 的高音质 16kHz 语音编码
try:
    import opuslib
    OPUS_AVAILABLE = True
except ImportError:
    OPUS_AVAILABLE = False


# ==========================================
# DraARLv1 协议常量定义 (参考 Protocol.md)
# ==========================================
DRAARL_VERSION = b"DraA"
DRAARL_HEADER_SIZE = 93

# 数据包类型
DRAARL_TYPE_CONTROL       = 0  # 控制指令
DRAARL_TYPE_HEARTBEAT     = 2  # 心跳包
DRAARL_TYPE_CONFIG        = 3  # 设备配置
DRAARL_TYPE_TEXT_MESSAGE  = 4  # 文本消息
DRAARL_TYPE_OPUS_16K      = 5  # Opus 16K 语音
DRAARL_TYPE_SERVER_VOICE  = 6  # 服务器互联语音
DRAARL_TYPE_AT_PASSTHROUGH = 7 # AT 透传

# 设备型号
DRAARL_DEV_MODEL_WECHAT_MINI = 100  # 微信小程序
DRAARL_DEV_MODEL_ANDROID     = 101  # Android 客户端
DRAARL_DEV_MODEL_IOS         = 102  # iOS 客户端
DRAARL_DEV_MODEL_WINDOWS     = 103  # Windows 客户端
DRAARL_DEV_MODEL_BROWSER     = 105  # 浏览器客户端
DRAARL_DEV_MODEL_INTERCONNECT = 106 # 互联设备


# ==========================================
# 核心协议与网络通信类
# ==========================================
class DraARLv1Client:
    """
    DraARLv1 协议客户端 - 支持独立配置 SSID

    协议头部结构 (93 字节):
    | 偏移 | 长度 | 字段名        |
    |------|------|---------------|
    | 0    | 4B   | Version       | "DraA"
    | 4    | 2B   | Length        | 报文总长度
    | 6    | 32B  | Username      | 用户名
    | 38   | 10B  | DevicePassword| 设备准入密码
    | 48   | 1B   | Type          | 数据包类型
    | 49   | 1B   | Status        | 状态字节
    | 50   | 2B   | SeqNum        | 序列号
    | 52   | 1B   | DevModel      | 设备型号
    | 53   | 1B   | SSID          | 设备子号
    | 54   | 3B   | DMRID         | DMR ID
    | 57   | 32B  | CallSign      | 呼号（服务器填充，设备发送时留空）
    | 89   | 4B   | Reserved      | 保留
    | 93   | 变长  | DATA          | 负载数据
    """
    def __init__(self, server_ip, server_port, username, device_password, ssid, dmrid_int, log_callback, color_tag=None):
        self.server_addr = (server_ip, server_port)
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.log = log_callback
        self.color_tag = color_tag  # 用于区分不同设备的日志颜色

        # --- 协议头部基础字段装配 ---
        self.version = DRAARL_VERSION
        self.username = username.encode('utf-8')[:32].ljust(32, b'\x00')
        self.device_password = device_password.encode('ascii')[:10].ljust(10, b'\x00')

        try:
            self.dmrid = int(dmrid_int).to_bytes(3, 'big')
        except OverflowError:
            self._log("[错误] DMRID 数值溢出，已重置为 0")
            self.dmrid = b'\x00\x00\x00'

        self.callsign = b'\x00' * 32
        self.ssid = int(ssid) & 0xFF
        self.dev_model = DRAARL_DEV_MODEL_WINDOWS
        self.pkt_count = 0

        # --- 运行状态与 PTT 控制 ---
        self.running = False
        self.is_transmitting = False

        # --- 音频引擎配置 ---
        self.audio_type = DRAARL_TYPE_OPUS_16K  # 只支持 Opus 16K
        if not OPUS_AVAILABLE:
            raise RuntimeError("未检测到 opuslib 库，无法运行。请安装: pip install opuslib")

        self.pyaudio_inst = pyaudio.PyAudio()
        self.audio_format = pyaudio.paInt16
        self.channels = 1
        self.rate = 16000
        self.chunk_size = 320
        self.opus_encoder = opuslib.Encoder(self.rate, self.channels, opuslib.APPLICATION_VOIP)
        self.opus_decoder = opuslib.Decoder(self.rate, self.channels)
        self._log(f"[SSID-{self.ssid}] 音频引擎: Opus 16kHz (Type 5)")

    def _log(self, message):
        """带颜色标签的日志输出"""
        if self.color_tag:
            self.log(f"[{self.color_tag}] {message}")
        else:
            self.log(message)

    def _pack_header(self, payload_length, pkt_type, status=0):
        total_length = DRAARL_HEADER_SIZE + payload_length
        self.pkt_count = (self.pkt_count + 1) & 0xFFFF
        reserved = b'\x00' * 4

        header = struct.pack(
            '>4sH32s10sBBHBB3s32s4s',
            self.version,
            total_length,
            self.username,
            self.device_password,
            pkt_type,
            status,
            self.pkt_count,
            self.dev_model,
            self.ssid,
            self.dmrid,
            self.callsign,
            reserved
        )
        return header

    def send_packet(self, pkt_type, payload=b'', status=0):
        try:
            header = self._pack_header(len(payload), pkt_type, status)
            self.sock.sendto(header + payload, self.server_addr)
        except Exception as e:
            self._log(f"[网络发送错误] {e}")

    def send_text_message(self, text):
        payload = text.encode('utf-8')
        self.send_packet(pkt_type=DRAARL_TYPE_TEXT_MESSAGE, payload=payload)
        self._log(f"[文字发出] {text}")

    def heartbeat_loop(self):
        while self.running:
            self.send_packet(pkt_type=DRAARL_TYPE_HEARTBEAT)
            time.sleep(2)

    def receive_loop(self):
        stream_out = self.pyaudio_inst.open(
            format=self.audio_format, channels=self.channels,
            rate=self.rate, output=True
        )

        last_sender = None  # 记录上一个发言者，避免重复打印

        while self.running:
            try:
                data, addr = self.sock.recvfrom(4096)
                if len(data) < DRAARL_HEADER_SIZE:
                    continue

                pkt_type = data[48]
                payload = data[DRAARL_HEADER_SIZE:]

                # 提取发送方 SSID (offset 53)
                sender_ssid = data[53]

                # 提取发送方呼号 (offset 57, 32字节)
                callsign = ""
                if len(data) >= 89:
                    callsign_bytes = data[57:89]
                    callsign = callsign_bytes.rstrip(b'\x00').decode('ascii', errors='replace')
                    if callsign and not hasattr(self, '_logged_callsign'):
                        self._log(f"[认证成功] 服务器返回呼号: {callsign}")
                        self._logged_callsign = True

                # --- 语音报文处理 ---
                if pkt_type == self.audio_type and len(payload) > 0:
                    # 半双工防回音：自己说话时不播放
                    if self.is_transmitting:
                        continue

                    # 显示发言者信息
                    sender_info = f"{callsign}-{sender_ssid}" if callsign else f"SSID-{sender_ssid}"
                    if last_sender != sender_info:
                        self._log(f"[接收] {sender_info} 正在发言...")
                        last_sender = sender_info

                    try:
                        if pkt_type == DRAARL_TYPE_OPUS_16K:
                            pcm_data = self.opus_decoder.decode(payload, self.chunk_size)
                            stream_out.write(pcm_data)
                    except Exception as e:
                        self._log(f"[音频解码失败] {e}")

                # --- 文本报文处理 ---
                elif pkt_type == DRAARL_TYPE_TEXT_MESSAGE:
                    msg_text = payload.decode('utf-8', errors='replace')
                    sender_info = f"{callsign}-{sender_ssid}" if callsign else f"SSID-{sender_ssid}"
                    self._log(f"[文字] {sender_info}: {msg_text}")

                # --- 语音结束检测（收到非语音包时重置发言者）---
                elif pkt_type in [DRAARL_TYPE_CONTROL, DRAARL_TYPE_HEARTBEAT]:
                    last_sender = None

            except socket.error:
                pass
            except Exception as e:
                self._log(f"[接收异常] {e}")

        stream_out.stop_stream()
        stream_out.close()

    def transmit_loop(self):
        try:
            stream_in = self.pyaudio_inst.open(
                format=self.audio_format, channels=self.channels,
                rate=self.rate, input=True, frames_per_buffer=self.chunk_size
            )
            self._log(f"[麦克风] 已就绪")
        except Exception as e:
            self._log(f"[严重错误] 麦克风初始化失败: {e}")
            return

        while self.running:
            try:
                pcm_data = stream_in.read(self.chunk_size, exception_on_overflow=False)

                if self.is_transmitting:
                    encoded_data = self.opus_encoder.encode(pcm_data, self.chunk_size)
                    self.send_packet(pkt_type=DRAARL_TYPE_OPUS_16K, payload=encoded_data)

            except Exception as e:
                self._log(f"[音频采集异常] {e}")
                time.sleep(0.1)

        stream_in.stop_stream()
        stream_in.close()

    def start(self):
        self.running = True
        threading.Thread(target=self.heartbeat_loop, daemon=True).start()
        threading.Thread(target=self.receive_loop, daemon=True).start()
        threading.Thread(target=self.transmit_loop, daemon=True).start()
        time.sleep(0.5)
        self.send_packet(pkt_type=DRAARL_TYPE_HEARTBEAT)

    def stop(self):
        self.running = False
        time.sleep(0.3)
        self.sock.close()
        self.pyaudio_inst.terminate()


# ==========================================
# 单设备控制面板
# ==========================================
class DevicePanel(ttk.LabelFrame):
    """单个设备的控制面板"""

    def __init__(self, parent, ssid, app, **kwargs):
        super().__init__(parent, text=f"设备 SSID-{ssid}", **kwargs)
        self.ssid = ssid
        self.app = app
        self.client = None
        self.is_connected = False
        self.space_pressed = False

        self._build_ui()

    def _build_ui(self):
        # 参数设置
        param_frame = ttk.Frame(self)
        param_frame.pack(fill=tk.X, padx=5, pady=2)

        ttk.Label(param_frame, text="用户名:").grid(row=0, column=0, sticky=tk.W)
        self.username_var = tk.StringVar(value="admin")
        ttk.Entry(param_frame, textvariable=self.username_var, width=12).grid(row=0, column=1, padx=2)

        ttk.Label(param_frame, text="密码:").grid(row=0, column=2, sticky=tk.W, padx=(5,0))
        self.password_var = tk.StringVar(value="Jb1M1PCk")
        ttk.Entry(param_frame, textvariable=self.password_var, width=10).grid(row=0, column=3, padx=2)

        ttk.Label(param_frame, text="DMRID:").grid(row=0, column=4, sticky=tk.W, padx=(5,0))
        self.dmrid_var = tk.StringVar(value="123456")
        ttk.Entry(param_frame, textvariable=self.dmrid_var, width=8).grid(row=0, column=5, padx=2)

        # 日志区域
        log_frame = ttk.Frame(self)
        log_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=2)

        self.log_area = scrolledtext.ScrolledText(log_frame, width=30, height=4, wrap=tk.WORD,
                                                   font=("Consolas", 9), state='disabled')
        self.log_area.pack(fill=tk.BOTH, expand=True)

        # 设置日志标签颜色
        self.log_area.tag_configure("blue", foreground="blue")
        self.log_area.tag_configure("green", foreground="green")

        # PTT 按钮
        self.btn_ptt = tk.Button(self, text="离线", font=("黑体", 11, "bold"),
                                  bg="lightgray", height=1, state=tk.DISABLED)
        self.btn_ptt.pack(fill=tk.X, padx=5, pady=3)

        self.btn_ptt.bind("<ButtonPress-1>", self.on_ptt_press)
        self.btn_ptt.bind("<ButtonRelease-1>", self.on_ptt_release)

    def log(self, message, tag=None):
        """线程安全的日志输出"""
        self.app.root.after(0, self._insert_log, message, tag)

    def _insert_log(self, message, tag=None):
        self.log_area.config(state='normal')
        self.log_area.insert(tk.END, message + "\n", tag)
        self.log_area.see(tk.END)
        self.log_area.config(state='disabled')

    def toggle_connection(self):
        if not self.is_connected:
            try:
                color_tag = "blue" if self.ssid == 1 else "green"
                self.client = DraARLv1Client(
                    server_ip=self.app.server_ip.get(),
                    server_port=int(self.app.server_port.get()),
                    username=self.username_var.get(),
                    device_password=self.password_var.get(),
                    ssid=self.ssid,
                    dmrid_int=self.dmrid_var.get() or 0,
                    log_callback=lambda msg, t=color_tag: self.log(msg, t),
                    color_tag=f"SSID-{self.ssid}"
                )
                self.client.start()

                self.is_connected = True
                self.btn_ptt.config(state=tk.NORMAL, text=f"按住说话 (SSID-{self.ssid})", bg="white")
                self.log(f"[系统] 已连接")

            except Exception as e:
                messagebox.showerror("连接错误", str(e))
        else:
            if self.client:
                self.client.stop()
            self.is_connected = False
            self.btn_ptt.config(state=tk.DISABLED, text="离线", bg="lightgray")
            self.log(f"[系统] 已断开")

    def on_ptt_press(self, event=None):
        if not self.is_connected or not self.client:
            return
        self.client.is_transmitting = True
        self.btn_ptt.config(bg="lightgreen", text="发射中...")
        self.log(f">>> [开始发射]")

    def on_ptt_release(self, event=None):
        if not self.is_connected or not self.client:
            return
        self.client.is_transmitting = False
        self.btn_ptt.config(bg="white", text=f"按住说话 (SSID-{self.ssid})")
        self.log(f"<<< [停止发射]")

    def stop(self):
        if self.client:
            self.client.stop()


# ==========================================
# 主应用程序
# ==========================================
class DualDeviceApp:
    """双设备模拟器主应用"""

    def __init__(self, root):
        self.root = root
        self.root.title("DraARLv1 双设备模拟器 (SSID 1 & 2)")
        self.root.geometry("1000x550")
        self.root.minsize(700, 450)
        self.root.protocol("WM_DELETE_WINDOW", self.on_closing)

        self._build_ui()

    def _build_ui(self):
        # 顶部：服务器配置
        server_frame = ttk.LabelFrame(self.root, text="服务器配置", padding=(10, 5))
        server_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(server_frame, text="服务器IP:").grid(row=0, column=0, sticky=tk.W)
        self.server_ip = tk.StringVar(value="127.0.0.1")
        ttk.Entry(server_frame, textvariable=self.server_ip, width=15).grid(row=0, column=1, padx=5)

        ttk.Label(server_frame, text="端口:").grid(row=0, column=2, sticky=tk.W, padx=(10,0))
        self.server_port = tk.StringVar(value="60050")
        ttk.Entry(server_frame, textvariable=self.server_port, width=8).grid(row=0, column=3, padx=5)

        # 快捷按钮
        ttk.Button(server_frame, text="全部连接", command=self.connect_all).grid(row=0, column=4, padx=10)
        ttk.Button(server_frame, text="全部断开", command=self.disconnect_all).grid(row=0, column=5, padx=5)

        # 中部：两个设备面板并排
        devices_frame = ttk.Frame(self.root)
        devices_frame.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)

        # 左侧设备 (SSID 1)
        self.device1 = DevicePanel(devices_frame, ssid=1, app=self)
        self.device1.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=(0, 5))

        # 右侧设备 (SSID 2)
        self.device2 = DevicePanel(devices_frame, ssid=2, app=self)
        self.device2.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=(5, 0))

        # 底部：键盘快捷键说明
        help_frame = ttk.LabelFrame(self.root, text="快捷键", padding=(10, 5))
        help_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(help_frame, text="[1] = SSID-1 发射").pack(side=tk.LEFT, padx=20)
        ttk.Label(help_frame, text="[2] = SSID-2 发射").pack(side=tk.LEFT, padx=20)
        ttk.Label(help_frame, text="松开即停止").pack(side=tk.LEFT, padx=20)

        # 绑定全局快捷键
        self.root.bind("<KeyPress-1>", lambda e: self.device1.on_ptt_press())
        self.root.bind("<KeyRelease-1>", lambda e: self.device1.on_ptt_release())
        self.root.bind("<KeyPress-2>", lambda e: self.device2.on_ptt_press())
        self.root.bind("<KeyRelease-2>", lambda e: self.device2.on_ptt_release())

        # 也支持空格键控制当前焦点的设备
        self.root.bind("<KeyPress-space>", self._on_space_press)
        self.root.bind("<KeyRelease-space>", self._on_space_release)

    def _on_space_press(self, event):
        # 空格键控制已连接的第一个设备
        if self.device1.is_connected:
            self.device1.on_ptt_press()
        elif self.device2.is_connected:
            self.device2.on_ptt_press()

    def _on_space_release(self, event):
        self.device1.on_ptt_release()
        self.device2.on_ptt_release()

    def connect_all(self):
        if not self.device1.is_connected:
            self.device1.toggle_connection()
        if not self.device2.is_connected:
            self.device2.toggle_connection()

    def disconnect_all(self):
        if self.device1.is_connected:
            self.device1.toggle_connection()
        if self.device2.is_connected:
            self.device2.toggle_connection()

    def on_closing(self):
        self.device1.stop()
        self.device2.stop()
        self.root.destroy()


# ==========================================
# 应用程序入口
# ==========================================
if __name__ == "__main__":
    root = tk.Tk()
    app = DualDeviceApp(root)
    root.focus_set()
    root.mainloop()
