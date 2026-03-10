import socket
import struct
import threading
import time
import pyaudio
import audioop
import tkinter as tk
from tkinter import ttk, scrolledtext, messagebox

# 尝试导入 opuslib，用于支持 Type 8 的高音质 16kHz 语音编码
try:
    import opuslib
    OPUS_AVAILABLE = True
except ImportError:
    OPUS_AVAILABLE = False


# ==========================================
# 核心协议与网络通信类
# ==========================================
class NRL2Client:
    def __init__(self, server_ip, server_port, callsign, ssid, dmrid_int, password_str, audio_type, log_callback):
        """
        初始化对讲机客户端的核心参数和音频流配置
        """
        self.server_addr = (server_ip, server_port)
        # 使用 UDP 协议进行通信
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        # 绑定前端传递进来的日志刷新函数，用于线程安全的 UI 更新
        self.log = log_callback 
        
        # --- 协议头部基础字段装配 ---
        self.version = b"NRL2"
        try:
            # 将十进制的 DMRID 转换为 3 字节的大端序字节流
            self.dmrid = int(dmrid_int).to_bytes(3, 'big')
        except OverflowError:
            self.log("[错误] DMRID 数值溢出 (最大16777215)，已重置为 0")
            self.dmrid = b'\x00\x00\x00'
            
        # 密码不足 11 位用 \x00 补齐，呼号不足 6 位用 \x00 补齐
        self.password = password_str.encode('ascii').ljust(11, b'\x00')
        self.callsign = callsign.encode('ascii').ljust(6, b'\x00')
        self.ssid = int(ssid)
        self.dev_model = 100
        self.pkt_count = 0
        
        # --- 运行状态与 PTT 控制 ---
        self.running = False
        self.is_transmitting = False # PTT 状态锁：为 True 时才将采集的音频发送到网络
        
        # --- 音频引擎配置 ---
        self.audio_type = int(audio_type)
        self.pyaudio_inst = pyaudio.PyAudio()
        self.audio_format = pyaudio.paInt16
        self.channels = 1
        
        # 根据选定的协议 Type 初始化对应的采样率和编解码器
        if self.audio_type == 8:
            if not OPUS_AVAILABLE:
                raise RuntimeError("未检测到 opuslib 库，无法使用 Type 8 编码。")
            self.rate = 16000
            self.chunk_size = 320 # 16kHz 下 20ms 帧长对应 320 个采样点
            self.opus_encoder = opuslib.Encoder(self.rate, self.channels, opuslib.APPLICATION_VOIP)
            self.opus_decoder = opuslib.Decoder(self.rate, self.channels)
            self.log("[系统初始化] 音频引擎加载完毕: Opus 16kHz (Type 8)")
        elif self.audio_type == 1:
            self.rate = 8000
            self.chunk_size = 160 # 8kHz 下 20ms 帧长对应 160 个采样点
            self.log("[系统初始化] 音频引擎加载完毕: G.711 A-law 8kHz (Type 1)")

    def _pack_header(self, payload_length, pkt_type):
        """
        按照 NRL2 规范组装固定 48 字节长度的报文头部 (大端序)
        """
        total_length = 48 + payload_length
        # 报文计数器自增，超过 65535 自动归零
        self.pkt_count = (self.pkt_count + 1) & 0xFFFF
        reserved = b'\x00' * 14 # 补齐 14 字节的保留扩展位
        
        # 打包前 46 字节
        header_46 = struct.pack(
            '>4sH3s11sBBH6sBB14s',
            self.version, total_length, self.dmrid, self.password,
            pkt_type, 0, self.pkt_count, self.callsign,
            self.ssid, self.dev_model, reserved
        )
        # 追加 2 字节 CRC (目前暂填 0x0000 试探服务器容错，若需严格校验可在此替换算法)
        return header_46 + struct.pack('>H', 0x0000)

    def send_packet(self, pkt_type, payload=b''):
        """底层网络发送逻辑，负责拼接头部和数据并投递"""
        try:
            header = self._pack_header(len(payload), pkt_type)
            self.sock.sendto(header + payload, self.server_addr)
        except Exception as e:
            self.log(f"[网络发送错误] {e}")

    def send_text_message(self, text, msg_subtype="text"):
        """封装并发送 Type 5 的格式化文本消息"""
        formatted_msg = f"[{msg_subtype}]{text}"
        payload = formatted_msg.encode('utf-8')
        self.send_packet(pkt_type=5, payload=payload)
        self.log(f"[文字发出] {formatted_msg}")

    def heartbeat_loop(self):
        """心跳守护线程：维持 UDP NAT 穿透和设备在线状态 (2秒一次)"""
        while self.running:
            self.send_packet(pkt_type=2)
            time.sleep(2)

    def receive_loop(self):
        """接收守护线程：监听网络端口，解析报文并驱动扬声器播放"""
        stream_out = self.pyaudio_inst.open(
            format=self.audio_format, channels=self.channels,
            rate=self.rate, output=True
        )
        
        while self.running:
            try:
                # 阻塞接收网络数据
                data, addr = self.sock.recvfrom(4096)
                if len(data) < 48: 
                    continue # 丢弃长度不合法的畸形包
                
                # 提取协议类型和负载数据
                pkt_type = data[20]
                payload = data[48:]
                
                # --- 语音报文处理分支 ---
                if pkt_type == self.audio_type and len(payload) > 0:
                    
                    # 【逻辑关键点：半双工防回音机制】
                    # 如果你和别人联机对讲，请把下面两行代码取消注释，避免听到自己的回音。
                    # 目前处于注释状态，是为了方便你连接自己的服务器进行单机的“录音回环测试”。
                    # if self.is_transmitting:
                    #     continue
                        
                    try:
                        # 根据音频类型进行解码，还原为 PCM 原始音频数据
                        if pkt_type == 8:
                            pcm_data = self.opus_decoder.decode(payload, self.chunk_size)
                        elif pkt_type == 1:
                            pcm_data = audioop.alaw2lin(payload, 2)
                            
                        # 写入扬声器缓冲区进行播放
                        stream_out.write(pcm_data)
                    except Exception as e:
                        self.log(f"[音频解码失败] {e} (可能原因：网络丢包导致帧损坏)")
                        
                # --- 文本报文处理分支 ---
                elif pkt_type == 5:
                    msg_text = payload.decode('utf-8', errors='replace')
                    self.log(f"[收到文字] {addr[0]}: {msg_text}")
                    
            except socket.error:
                pass # 忽略 Socket 在非阻塞或关闭时的合法异常
            except Exception as e:
                self.log(f"[接收线程未捕获异常] {e}")
                
        # 清理音频输出流资源
        stream_out.stop_stream()
        stream_out.close()

    def transmit_loop(self):
        """发送守护线程：持续采集麦克风，并根据 PTT 状态决定是否编码发送"""
        stream_in = self.pyaudio_inst.open(
            format=self.audio_format, channels=self.channels,
            rate=self.rate, input=True, frames_per_buffer=self.chunk_size
        )
        self.log(f"[麦克风状态] 已就绪 (当前静音)。按住 PTT 按钮或空格键开始说话...")
        
        while self.running:
            try:
                # 必须持续读取麦克风数据以清空底层 Buffer，防止缓冲区溢出崩溃
                pcm_data = stream_in.read(self.chunk_size, exception_on_overflow=False)
                
                # 仅当按下 PTT 键 (is_transmitting 为 True) 时，才执行编码和网络发送
                if self.is_transmitting:
                    if self.audio_type == 8:
                        encoded_data = self.opus_encoder.encode(pcm_data, self.chunk_size)
                    elif self.audio_type == 1:
                        encoded_data = audioop.lin2alaw(pcm_data, 2)
                        
                    # 组装 Type 8 或 Type 1 报文投递到网络
                    self.send_packet(pkt_type=self.audio_type, payload=encoded_data)
                    
            except Exception as e:
                pass # 忽略音频输入流偶发的断流异常
                
        # 清理音频输入流资源
        stream_in.stop_stream()
        stream_in.close()

    def start(self):
        """启动所有相关线程并发送上线指令"""
        self.running = True
        threading.Thread(target=self.heartbeat_loop, daemon=True).start()
        threading.Thread(target=self.receive_loop, daemon=True).start()
        threading.Thread(target=self.transmit_loop, daemon=True).start()
        
        # 建立连接后稍微延迟，发送加入组 (Type 7) 指令
        time.sleep(0.5)
        self.send_packet(pkt_type=7)

    def stop(self):
        """安全停止所有线程并释放硬件资源"""
        self.running = False
        time.sleep(0.3) 
        self.sock.close()
        self.pyaudio_inst.terminate()


# ==========================================
# Tkinter 图形界面与交互逻辑类
# ==========================================
class NRL2App:
    def __init__(self, root):
        self.root = root
        self.root.title("NRL2 模拟对讲终端")
        self.root.geometry("500x750")
        self.root.protocol("WM_DELETE_WINDOW", self.on_closing) # 绑定关闭窗口事件
        
        self.client = None
        self.is_connected = False
        
        # 键盘空格键防连发状态锁 (防止操作系统一直发送 KeyPress 事件)
        self.space_pressed = False
        
        self._build_ui()
        self.log_to_gui("就绪。请配置参数并点击[连接]。\n连接后，按住底部大按钮或键盘【空格键】进行对讲。")

    def _build_ui(self):
        """构建界面的各项控件分布"""
        # --- 1. 参数设置面板 ---
        param_frame = ttk.LabelFrame(self.root, text="连接参数", padding=(10, 5))
        param_frame.pack(fill=tk.X, padx=10, pady=5)
        
        # 设定默认输入参数 (默认 IP 指向本地，方便你直接进行回环测试)
        self.vars = {
            'IP': tk.StringVar(value="127.0.0.1"), 
            '端口': tk.StringVar(value="60050"),
            '呼号': tk.StringVar(value="BH5UVN"),
            'SSID': tk.StringVar(value="100"),
            'DMRID': tk.StringVar(value="123456"),
            '密码': tk.StringVar(value="")
        }
        
        row, col = 0, 0
        for label_text, string_var in self.vars.items():
            ttk.Label(param_frame, text=label_text+":").grid(row=row, column=col, sticky=tk.W, pady=2, padx=5)
            ttk.Entry(param_frame, textvariable=string_var, width=15).grid(row=row, column=col+1, pady=2, padx=5)
            col += 2
            if col > 2:
                col, row = 0, row + 1

        # 音频编码格式单选框
        audio_frame = ttk.Frame(param_frame)
        audio_frame.grid(row=row, column=0, columnspan=4, sticky=tk.W, pady=5, padx=5)
        ttk.Label(audio_frame, text="音频编码:").pack(side=tk.LEFT)
        self.audio_var = tk.StringVar(value="8" if OPUS_AVAILABLE else "1")
        r1 = ttk.Radiobutton(audio_frame, text="Opus (Type 8)", variable=self.audio_var, value="8")
        r2 = ttk.Radiobutton(audio_frame, text="G.711 (Type 1)", variable=self.audio_var, value="1")
        r1.pack(side=tk.LEFT, padx=5)
        r2.pack(side=tk.LEFT, padx=5)
        if not OPUS_AVAILABLE:
            r1.state(['disabled']) # 若未安装 opuslib 则禁用该选项

        # 连接控制按钮
        self.btn_connect = ttk.Button(self.root, text="启动连接", command=self.toggle_connection)
        self.btn_connect.pack(fill=tk.X, padx=10, pady=5)

        # --- 2. 状态/日志打印面板 ---
        log_frame = ttk.LabelFrame(self.root, text="运行日志", padding=(10, 5))
        log_frame.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)
        
        self.log_area = scrolledtext.ScrolledText(log_frame, state='disabled', wrap=tk.WORD, font=("Consolas", 9))
        self.log_area.pack(fill=tk.BOTH, expand=True)
        
        # --- 3. PTT 对讲交互面板 ---
        ptt_frame = ttk.LabelFrame(self.root, text="对讲控制 (PTT)", padding=(10, 10))
        ptt_frame.pack(fill=tk.X, padx=10, pady=10)
        
        # 使用 tk.Button 以支持动态修改背景颜色，提供明确的视觉反馈
        self.btn_ptt = tk.Button(ptt_frame, text="🎤 离线状态", font=("黑体", 14, "bold"), bg="lightgray", state=tk.DISABLED)
        self.btn_ptt.pack(fill=tk.BOTH, expand=True, ipady=20)
        
        # 绑定鼠标左右键按下与松开事件
        self.btn_ptt.bind("<ButtonPress-1>", self.on_ptt_press)
        self.btn_ptt.bind("<ButtonRelease-1>", self.on_ptt_release)
        
        # 绑定全局键盘空格键事件
        self.root.bind("<KeyPress-space>", self.on_ptt_press)
        self.root.bind("<KeyRelease-space>", self.on_ptt_release)

    def log_to_gui(self, message):
        """将日志操作委托给主线程执行，保障线程安全"""
        self.root.after(0, self._insert_log, message)

    def _insert_log(self, message):
        """实际执行日志文本插入的底层方法"""
        self.log_area.config(state='normal')
        self.log_area.insert(tk.END, message + "\n")
        self.log_area.see(tk.END) # 自动滚动到底部
        self.log_area.config(state='disabled')

    def toggle_connection(self):
        """处理连接与断开的切换状态"""
        if not self.is_connected:
            try:
                # 实例化核心协议类
                self.client = NRL2Client(
                    server_ip=self.vars['IP'].get(),
                    server_port=int(self.vars['端口'].get()),
                    callsign=self.vars['呼号'].get(),
                    ssid=self.vars['SSID'].get(),
                    dmrid_int=self.vars['DMRID'].get() or 0,
                    password_str=self.vars['密码'].get(),
                    audio_type=self.audio_var.get(),
                    log_callback=self.log_to_gui
                )
                self.client.start()
                
                # 更新前端 UI 状态
                self.is_connected = True
                self.btn_connect.config(text="断开连接")
                self.btn_ptt.config(state=tk.NORMAL, text="🎤 按住说话 (空格键 / 左键)", bg="white")
                self.log_to_gui(f"\n[系统] 已连接至服务器: {self.vars['IP'].get()}:{self.vars['端口'].get()}")
                
            except Exception as e:
                messagebox.showerror("连接错误", str(e))
        else:
            if self.client:
                self.client.stop()
            self.is_connected = False
            self.btn_connect.config(text="启动连接")
            self.btn_ptt.config(state=tk.DISABLED, text="🎤 离线状态", bg="lightgray")
            self.log_to_gui("\n[系统] 连接已断开。")

    def on_ptt_press(self, event):
        """按下 PTT 键，通知底层网络开始将录音发包"""
        if not self.is_connected or not self.client: 
            return
            
        # 过滤键盘的长按连发信号
        if hasattr(event, 'keysym') and event.keysym == 'space':
            if self.space_pressed:
                return 
            self.space_pressed = True
            
        self.client.is_transmitting = True
        self.btn_ptt.config(bg="lightgreen", text="🔊 正在发射中...")
        self.log_to_gui(">>> [开始发射语音]")

    def on_ptt_release(self, event):
        """松开 PTT 键，通知底层网络停止发包"""
        if not self.is_connected or not self.client: 
            return
            
        if hasattr(event, 'keysym') and event.keysym == 'space':
            self.space_pressed = False
            
        self.client.is_transmitting = False
        self.btn_ptt.config(bg="white", text="🎤 按住说话 (空格键 / 左键)")
        self.log_to_gui("<<< [停止发射]")

    def on_closing(self):
        """捕获窗口关闭事件，确保底层线程和端口资源被安全回收"""
        if self.is_connected and self.client:
            self.client.stop()
        self.root.destroy()

# ==========================================
# 应用程序入口
# ==========================================
if __name__ == "__main__":
    root = tk.Tk()
    app = NRL2App(root)
    # 将焦点强制设定在主窗口，保障启动后按空格键能立即响应
    root.focus_set() 
    root.mainloop()