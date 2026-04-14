"""
DraARL 调试客户端
支持 UDP普通设备、UDP JWT 两种连接方式
"""

import os
import random
import sys
import threading
import tkinter as tk
from tkinter import messagebox, scrolledtext, ttk

# 添加当前目录到路径
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from client.udp_device import UDPDeviceClient
from client.udp_jwt import UDPJWTClient
from client.serial_device import SerialClient
from protocol import DevModel, get_dev_model_name
from utils.jwt_gen import generate_jwt
from utils.http_client import HTTPClient




class ClientPanel(ttk.LabelFrame):
    """单个客户端控制面板"""

    def __init__(self, parent, panel_name: str, client_type: str, app, **kwargs):
        super().__init__(parent, text=panel_name, **kwargs)
        self.panel_name = panel_name
        self.client_type = client_type
        self.app = app
        self.client = None
        self.is_connected = False
        self.root = parent  # 保存 parent 用于创建子窗口
        self.bound_dmrid = 0

        self._build_ui()

    def _build_ui(self):
        """构建 UI"""
        # 参数配置区
        param_frame = ttk.Frame(self)
        param_frame.pack(fill=tk.X, padx=5, pady=2)

        if self.client_type == "udp_device":
            self._build_udp_device_params(param_frame)
        elif self.client_type == "udp_jwt":
            self._build_udp_jwt_params(param_frame)
        elif self.client_type == "serial_device":
            self._build_serial_device_params(param_frame)

        # 日志区域
        log_frame = ttk.Frame(self)
        log_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=2)

        self.log_area = scrolledtext.ScrolledText(
            log_frame, width=40, height=8, wrap=tk.WORD,
            font=("Consolas", 9), state='disabled'
        )
        self.log_area.pack(fill=tk.BOTH, expand=True)

        # 控制按钮区
        btn_frame = ttk.Frame(self)
        btn_frame.pack(fill=tk.X, padx=5, pady=3)

        self.btn_connect = ttk.Button(btn_frame, text="连接", command=self.toggle_connection)
        self.btn_connect.pack(side=tk.LEFT, padx=2)

        self.btn_ptt = tk.Button(
            btn_frame, text="PTT", font=("黑体", 10, "bold"),
            bg="lightgray", width=8, state=tk.DISABLED
        )
        self.btn_ptt.pack(side=tk.LEFT, padx=5)
        self.btn_ptt.bind("<ButtonPress-1>", self.on_ptt_press)
        self.btn_ptt.bind("<ButtonRelease-1>", self.on_ptt_release)

        # 文本发送
        text_frame = ttk.Frame(self)
        text_frame.pack(fill=tk.X, padx=5, pady=2)

        self.text_entry = ttk.Entry(text_frame, width=25)
        self.text_entry.pack(side=tk.LEFT, padx=2)
        self.text_entry.bind("<Return>", self.send_text)

        ttk.Button(text_frame, text="发送", command=self.send_text).pack(side=tk.LEFT, padx=2)

        # Config 按钮（UDP 普通设备和串口设备）
        if self.client_type in ("udp_device", "serial_device"):
            ttk.Button(text_frame, text="Config", command=self.show_config).pack(side=tk.LEFT, padx=5)
        if self.client_type == "udp_device":
            ttk.Button(text_frame, text="动态绑定", command=self.show_dynamic_bind).pack(side=tk.LEFT, padx=5)

        # 群组切换（仅 JWT 客户端）
        if self.client_type == "udp_jwt":
            group_frame = ttk.Frame(self)
            group_frame.pack(fill=tk.X, padx=5, pady=2)

            ttk.Label(group_frame, text="群组:").pack(side=tk.LEFT)
            self.group_var = tk.StringVar(value="999")
            ttk.Entry(group_frame, textvariable=self.group_var, width=6).pack(side=tk.LEFT, padx=2)
            ttk.Button(group_frame, text="切换", command=self.switch_group).pack(side=tk.LEFT, padx=2)

    def _build_udp_device_params(self, parent):
        """UDP 普通设备参数"""
        ttk.Label(parent, text="用户:").grid(row=0, column=0, sticky=tk.W)
        self.username_var = tk.StringVar(value="admin")
        ttk.Entry(parent, textvariable=self.username_var, width=10).grid(row=0, column=1, padx=2)

        ttk.Label(parent, text="密码:").grid(row=0, column=2, sticky=tk.W, padx=(5,0))
        self.password_var = tk.StringVar(value="iybBo07f")
        ttk.Entry(parent, textvariable=self.password_var, width=10).grid(row=0, column=3, padx=2)

        ttk.Label(parent, text="SSID:").grid(row=1, column=0, sticky=tk.W)
        self.ssid_var = tk.StringVar(value="1")
        ttk.Entry(parent, textvariable=self.ssid_var, width=5).grid(row=1, column=1, padx=2, sticky=tk.W)

        ttk.Label(parent, text="型号:").grid(row=1, column=2, sticky=tk.W, padx=(5,0))
        self.devmodel_var = tk.StringVar(value="107")
        devmodel_combo = ttk.Combobox(parent, textvariable=self.devmodel_var, width=10,
                                       values=["100", "106", "107"])
        devmodel_combo.grid(row=1, column=3, padx=2)

        ttk.Label(parent, text="MAC:").grid(row=2, column=0, sticky=tk.W)
        self.mac_var = tk.StringVar(value=self._generate_random_mac())
        ttk.Entry(parent, textvariable=self.mac_var, width=14).grid(row=2, column=1, padx=2, sticky=tk.W)
        ttk.Button(parent, text="随机", command=self.refresh_mac).grid(row=2, column=2, padx=(5, 0), sticky=tk.W)

    def _build_serial_device_params(self, parent):
        """串口设备参数"""
        # 第一行：串口和波特率
        ttk.Label(parent, text="端口:").grid(row=0, column=0, sticky=tk.W)
        self.port_var = tk.StringVar(value="COM7")
        port_combo = ttk.Combobox(parent, textvariable=self.port_var, width=8,
                                  values=SerialClient.list_ports())
        port_combo.grid(row=0, column=1, padx=2)

        ttk.Label(parent, text="波特率:").grid(row=0, column=2, sticky=tk.W, padx=(5,0))
        self.baudrate_var = tk.StringVar(value="921600")
        ttk.Entry(parent, textvariable=self.baudrate_var, width=8).grid(row=0, column=3, padx=2)

        # 第二行：用户名和密码
        ttk.Label(parent, text="用户:").grid(row=1, column=0, sticky=tk.W)
        self.username_var = tk.StringVar(value="admin")
        ttk.Entry(parent, textvariable=self.username_var, width=10).grid(row=1, column=1, padx=2)

        ttk.Label(parent, text="密码:").grid(row=1, column=2, sticky=tk.W, padx=(5,0))
        self.password_var = tk.StringVar(value="iybBo07f")
        ttk.Entry(parent, textvariable=self.password_var, width=10).grid(row=1, column=3, padx=2)

        # 第三行：SSID和型号
        ttk.Label(parent, text="SSID:").grid(row=2, column=0, sticky=tk.W)
        self.ssid_var = tk.StringVar(value="1")
        ttk.Entry(parent, textvariable=self.ssid_var, width=5).grid(row=2, column=1, padx=2, sticky=tk.W)

        ttk.Label(parent, text="型号:").grid(row=2, column=2, sticky=tk.W, padx=(5,0))
        self.devmodel_var = tk.StringVar(value="107")
        devmodel_combo = ttk.Combobox(parent, textvariable=self.devmodel_var, width=10,
                                       values=["100", "106", "107"])
        devmodel_combo.grid(row=2, column=3, padx=2)

        ttk.Label(parent, text="MAC:").grid(row=3, column=0, sticky=tk.W)
        self.mac_var = tk.StringVar(value=self._generate_random_mac())
        ttk.Entry(parent, textvariable=self.mac_var, width=14).grid(row=3, column=1, padx=2, sticky=tk.W)
        ttk.Button(parent, text="随机", command=self.refresh_mac).grid(row=3, column=2, padx=(5, 0), sticky=tk.W)

    def _build_udp_jwt_params(self, parent):
        """UDP JWT 参数"""
        ttk.Label(parent, text="型号:").grid(row=0, column=0, sticky=tk.W)
        self.devmodel_var = tk.StringVar(value="103")
        devmodel_combo = ttk.Combobox(parent, textvariable=self.devmodel_var, width=12,
                                       values=["101", "102", "103", "104"])
        devmodel_combo.grid(row=0, column=1, padx=2)

        ttk.Label(parent, text="(SSID=型号)").grid(row=0, column=2, sticky=tk.W)

        ttk.Label(parent, text="Token:").grid(row=1, column=0, sticky=tk.W)
        self.token_var = tk.StringVar(value="")
        ttk.Entry(parent, textvariable=self.token_var, width=30).grid(row=1, column=1, columnspan=2, padx=2, sticky=tk.W)

        ttk.Button(parent, text="生成", command=self._generate_token).grid(row=1, column=3, padx=2)

    def _generate_token(self):
        """生成 JWT Token"""
        username = "admin"
        if hasattr(self, 'username_var'):
            username = self.username_var.get() or "admin"
        token = generate_jwt(username, ["user"])
        self.token_var.set(token)
        self.log(f"[Token] 已生成: {token[:50]}...")

    def _generate_random_mac(self) -> str:
        """生成一个本地管理 MAC，便于重复测试动态绑定。"""
        octets = [0x02, 0xAA, 0xBB]
        octets.extend(random.randint(0x00, 0xFF) for _ in range(3))
        return ":".join(f"{value:02X}" for value in octets)

    def refresh_mac(self):
        """刷新当前面板的模拟 MAC"""
        if hasattr(self, 'mac_var'):
            new_mac = self._generate_random_mac()
            self.mac_var.set(new_mac)
            self.log(f"[MAC] 已更新为 {new_mac}")

    def log(self, message: str):
        """线程安全日志输出"""
        self.app.root.after(0, self._insert_log, message)

    def _insert_log(self, message: str):
        """插入日志"""
        self.log_area.config(state='normal')
        self.log_area.insert(tk.END, message + "\n")
        self.log_area.see(tk.END)
        self.log_area.config(state='disabled')

    def toggle_connection(self):
        """切换连接状态"""
        if not self.is_connected:
            self._connect()
        else:
            self._disconnect()

    def _connect(self):
        """建立连接"""
        try:
            server_ip = self.app.server_ip.get()
            server_port = int(self.app.udp_port.get())

            if self.client_type == "udp_device":
                self.client = UDPDeviceClient(
                    server_ip=server_ip,
                    server_port=server_port,
                    username=self.username_var.get(),
                    device_password=self.password_var.get(),
                    ssid=int(self.ssid_var.get()),
                    mac=self.mac_var.get() if hasattr(self, 'mac_var') else "",
                    dmrid=self.bound_dmrid,
                    dev_model=int(self.devmodel_var.get()),
                    log_callback=self.log,
                    enable_audio=True
                )

            elif self.client_type == "udp_jwt":
                token = self.token_var.get()
                if not token:
                    self.log("[错误] 请先生成或输入 JWT Token")
                    return
                self.client = UDPJWTClient(
                    server_ip=server_ip,
                    server_port=server_port,
                    jwt_token=token,
                    dev_model=int(self.devmodel_var.get()),
                    log_callback=self.log,
                    enable_audio=True
                )

            elif self.client_type == "serial_device":
                self.client = SerialClient(
                    port=self.port_var.get(),
                    baudrate=int(self.baudrate_var.get()),
                    username=self.username_var.get(),
                    device_password=self.password_var.get(),
                    ssid=int(self.ssid_var.get()),
                    mac=self.mac_var.get() if hasattr(self, 'mac_var') else "",
                    dmrid=self.bound_dmrid,
                    dev_model=int(self.devmodel_var.get()),
                    log_callback=self.log,
                    enable_audio=True
                )

            # 启动连接
            if self.client.connect():
                self.is_connected = True
                self.btn_connect.config(text="断开")
                self.btn_ptt.config(state=tk.NORMAL, bg="white")
                self.log("[系统] 已连接")
            else:
                self.log("[系统] 连接失败")
                self.client = None

        except Exception as e:
            self.log(f"[错误] {e}")
            messagebox.showerror("连接错误", str(e))

    def _disconnect(self):
        """断开连接"""
        if self.client:
            self.client.disconnect()
        self.client = None
        self.is_connected = False
        self.btn_connect.config(text="连接")
        self.btn_ptt.config(state=tk.DISABLED, bg="lightgray")
        self.log("[系统] 已断开")

    def on_ptt_press(self, event=None):
        """PTT 按下"""
        if not self.is_connected or not self.client:
            return
        self.client.start_transmit()
        self.btn_ptt.config(bg="lightgreen")

    def on_ptt_release(self, event=None):
        """PTT 释放"""
        if not self.is_connected or not self.client:
            return
        self.client.stop_transmit()
        self.btn_ptt.config(bg="white")

    def send_text(self, event=None):
        """发送文本消息"""
        if not self.is_connected or not self.client:
            return
        text = self.text_entry.get().strip()
        if text:
            self.client.send_text(text)
            self.text_entry.delete(0, tk.END)

    def switch_group(self):
        """切换群组（JWT 客户端通过 HTTP API）"""
        if not hasattr(self, 'group_var'):
            return

        try:
            group_id = int(self.group_var.get())
        except ValueError:
            self.log("[错误] 群组 ID 必须是数字")
            return

        # 创建 HTTP 客户端
        server_ip = self.app.server_ip.get()
        http_port = self.app.http_port.get()
        http = HTTPClient(f"http://{server_ip}:{http_port}", log_callback=self.log)

        # 获取 Token
        token = None
        if hasattr(self, 'token_var'):
            token = self.token_var.get()

        if not token:
            token = generate_jwt("admin", ["user"])

        http.set_token(token)

        # 调用切换群组 API
        dev_model = 103  # Windows
        if hasattr(self, 'devmodel_var'):
            try:
                dev_model = int(self.devmodel_var.get())
            except:
                pass

        http.update_radio_group(group_id, dev_model)

    def show_config(self):
        """显示 Config 配置窗口"""
        if not self.is_connected or not self.client:
            self.log("[错误] 请先连接")
            return

        # 创建配置窗口
        config_window = tk.Toplevel(self.root)
        config_window.title(f"Config - {self.panel_name}")
        config_window.geometry("420x720")
        config_window.resizable(False, False)

        # 主区域
        frame = ttk.Frame(config_window, padding=10)
        frame.pack(fill=tk.BOTH, expand=True)

        # === 当前配置显示区 ===
        display_frame = ttk.LabelFrame(frame, text="当前配置（服务器下发）", padding=10)
        display_frame.pack(fill=tk.X, pady=(0, 10))

        # 获取当前配置
        config = self.client.get_device_config()
        tone_mode_options = ["off", "ctcss", "cdcss_n", "cdcss_i"]

        def normalize_tone_mode(value: str) -> str:
            raw = str(value).strip().lower()
            if raw in ("", "off", "0"):
                return "off"
            if raw in ("ctcss", "1"):
                return "ctcss"
            if raw in ("cdcss_n", "cdcss-n", "2"):
                return "cdcss_n"
            if raw in ("cdcss_i", "cdcss-i", "3"):
                return "cdcss_i"
            return "off"

        def format_tone_mode(value: str) -> str:
            return {
                "off": "OFF",
                "ctcss": "CTCSS",
                "cdcss_n": "CDCSS_N",
                "cdcss_i": "CDCSS_I",
            }.get(normalize_tone_mode(value), "OFF")

        def format_rf_guard_enabled(value: str) -> str:
            return "开启" if str(value).strip() in ("1", "true", "True", "on") else "关闭"

        def format_seconds(value: str) -> str:
            return f"{value} 秒" if str(value).strip() else "-"

        # 配置项显示
        self.config_labels = {}
        display_items = [
            ("rx_freq", "接收频率", lambda v: f"{int(v)/1e6:.4f} MHz" if v else "-"),
            ("tx_freq", "发射频率", lambda v: f"{int(v)/1e6:.4f} MHz" if v else "-"),
            ("rx_ctcss", "接收亚音", lambda v: f"{float(v):.1f} Hz" if v and v != "0" and v != "0.0" else "关闭"),
            ("tx_ctcss", "发射亚音", lambda v: f"{float(v):.1f} Hz" if v and v != "0" and v != "0.0" else "关闭"),
            ("rx_tone_mode", "接收数字亚音模式", format_tone_mode),
            ("rx_tone_value", "接收数字亚音值", lambda v: v if v else "-"),
            ("tx_tone_mode", "发射数字亚音模式", format_tone_mode),
            ("tx_tone_value", "发射数字亚音值", lambda v: v if v else "-"),
            ("sql_level", "静噪等级", lambda v: f"{v}"),
            ("power_level", "功率等级", lambda v: {"1": "低", "3": "高"}.get(v, v)),
            ("tx_bandwidth", "发射带宽", lambda v: "窄带" if v == "1" else "宽带"),
            ("rf_guard_enabled", "发射保护", format_rf_guard_enabled),
            ("rf_guard_single_tx_limit_s", "单次发射上限", format_seconds),
            ("rf_guard_window_s", "统计窗口", format_seconds),
            ("rf_guard_max_tx_in_window_s", "窗口累计上限", format_seconds),
        ]

        for key, label, formatter in display_items:
            row = ttk.Frame(display_frame)
            row.pack(fill=tk.X, pady=1)

            ttk.Label(row, text=f"{label}:", width=12, anchor=tk.W).pack(side=tk.LEFT)
            value = config.get(key, "-")
            value_label = ttk.Label(row, text=formatter(value), font=("Consolas", 10), foreground="blue")
            value_label.pack(side=tk.LEFT, padx=5)
            self.config_labels[key] = value_label

        # === 编辑区 ===
        edit_frame = ttk.LabelFrame(frame, text="修改配置（本地上报）", padding=10)
        edit_frame.pack(fill=tk.BOTH, expand=True, pady=(0, 10))

        # 编辑控件
        self.config_vars = {}

        # 频率输入（显示和输入都用 MHz，内部转换）
        freq_row = ttk.Frame(edit_frame)
        freq_row.pack(fill=tk.X, pady=2)
        ttk.Label(freq_row, text="发射频率(MHz):", width=14, anchor=tk.W).pack(side=tk.LEFT)

        # Hz -> MHz 转换用于显示
        tx_freq_mhz = int(config.get('tx_freq', '439500000')) / 1e6
        rx_freq_mhz = int(config.get('rx_freq', '439500000')) / 1e6

        self.config_vars['tx_freq'] = tk.StringVar(value=f"{tx_freq_mhz:.4f}")
        tx_freq_entry = ttk.Entry(freq_row, textvariable=self.config_vars['tx_freq'], width=12)
        tx_freq_entry.pack(side=tk.LEFT, padx=5)
        ttk.Label(freq_row, text="接收(MHz):").pack(side=tk.LEFT, padx=(10, 0))
        self.config_vars['rx_freq'] = tk.StringVar(value=f"{rx_freq_mhz:.4f}")
        ttk.Entry(freq_row, textvariable=self.config_vars['rx_freq'], width=12).pack(side=tk.LEFT, padx=5)

        # 亚音输入
        ctcss_row = ttk.Frame(edit_frame)
        ctcss_row.pack(fill=tk.X, pady=2)
        ttk.Label(ctcss_row, text="发射亚音(Hz):", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['tx_ctcss'] = tk.StringVar(value=config.get('tx_ctcss', '0'))
        ttk.Entry(ctcss_row, textvariable=self.config_vars['tx_ctcss'], width=8).pack(side=tk.LEFT, padx=5)
        ttk.Label(ctcss_row, text="接收(Hz):").pack(side=tk.LEFT, padx=(10, 0))
        self.config_vars['rx_ctcss'] = tk.StringVar(value=config.get('rx_ctcss', '0'))
        ttk.Entry(ctcss_row, textvariable=self.config_vars['rx_ctcss'], width=8).pack(side=tk.LEFT, padx=5)
        ttk.Label(ctcss_row, text="0=关闭", foreground="gray").pack(side=tk.LEFT, padx=5)

        # 数字亚音模式
        tone_mode_row = ttk.Frame(edit_frame)
        tone_mode_row.pack(fill=tk.X, pady=2)
        ttk.Label(tone_mode_row, text="发射数字亚音:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['tx_tone_mode'] = tk.StringVar(value=normalize_tone_mode(config.get('tx_tone_mode', 'off')))
        ttk.Combobox(tone_mode_row, textvariable=self.config_vars['tx_tone_mode'],
                     values=tone_mode_options, width=9, state="readonly").pack(side=tk.LEFT, padx=5)
        ttk.Label(tone_mode_row, text="接收:").pack(side=tk.LEFT, padx=(10, 0))
        self.config_vars['rx_tone_mode'] = tk.StringVar(value=normalize_tone_mode(config.get('rx_tone_mode', 'off')))
        ttk.Combobox(tone_mode_row, textvariable=self.config_vars['rx_tone_mode'],
                     values=tone_mode_options, width=9, state="readonly").pack(side=tk.LEFT, padx=5)

        # 数字亚音值
        tone_value_row = ttk.Frame(edit_frame)
        tone_value_row.pack(fill=tk.X, pady=2)
        ttk.Label(tone_value_row, text="发射数字亚音值:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['tx_tone_value'] = tk.StringVar(value=config.get('tx_tone_value', '0'))
        ttk.Entry(tone_value_row, textvariable=self.config_vars['tx_tone_value'], width=8).pack(side=tk.LEFT, padx=5)
        ttk.Label(tone_value_row, text="接收值:").pack(side=tk.LEFT, padx=(10, 0))
        self.config_vars['rx_tone_value'] = tk.StringVar(value=config.get('rx_tone_value', '0'))
        ttk.Entry(tone_value_row, textvariable=self.config_vars['rx_tone_value'], width=8).pack(side=tk.LEFT, padx=5)

        # 静噪等级
        sql_row = ttk.Frame(edit_frame)
        sql_row.pack(fill=tk.X, pady=2)
        ttk.Label(sql_row, text="静噪等级:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['sql_level'] = tk.StringVar(value=config.get('sql_level', '3'))
        sql_scale = ttk.Scale(sql_row, from_=0, to=8, orient=tk.HORIZONTAL, length=100)
        sql_scale.set(int(config.get('sql_level', '3')))
        sql_scale.pack(side=tk.LEFT, padx=5)
        sql_label = ttk.Label(sql_row, text=config.get('sql_level', '3'), width=2)
        sql_label.pack(side=tk.LEFT)
        sql_scale.config(command=lambda v: sql_label.config(text=str(int(float(v)))))

        # 功率等级
        power_row = ttk.Frame(edit_frame)
        power_row.pack(fill=tk.X, pady=2)
        ttk.Label(power_row, text="功率等级:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['power_level'] = tk.StringVar(value=config.get('power_level', '3'))
        power_combo = ttk.Combobox(power_row, textvariable=self.config_vars['power_level'],
                                    values=["1", "3"], width=5, state="readonly")
        power_combo.pack(side=tk.LEFT, padx=5)
        ttk.Label(power_row, text="(1=低, 3=高)", foreground="gray").pack(side=tk.LEFT)

        # 带宽
        bw_row = ttk.Frame(edit_frame)
        bw_row.pack(fill=tk.X, pady=2)
        ttk.Label(bw_row, text="发射带宽:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['tx_bandwidth'] = tk.StringVar(value=config.get('tx_bandwidth', '2'))
        bw_combo = ttk.Combobox(bw_row, textvariable=self.config_vars['tx_bandwidth'],
                                 values=["1", "2"], width=5, state="readonly")
        bw_combo.pack(side=tk.LEFT, padx=5)
        ttk.Label(bw_row, text="(1=窄带, 2=宽带)", foreground="gray").pack(side=tk.LEFT)

        # 发射保护
        rf_guard_row = ttk.Frame(edit_frame)
        rf_guard_row.pack(fill=tk.X, pady=2)
        ttk.Label(rf_guard_row, text="发射保护:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['rf_guard_enabled'] = tk.BooleanVar(
            value=str(config.get('rf_guard_enabled', '1')).strip() in ("1", "true", "True", "on")
        )
        ttk.Checkbutton(
            rf_guard_row,
            text="启用",
            variable=self.config_vars['rf_guard_enabled']
        ).pack(side=tk.LEFT, padx=5)

        rf_guard_limit_row = ttk.Frame(edit_frame)
        rf_guard_limit_row.pack(fill=tk.X, pady=2)
        ttk.Label(rf_guard_limit_row, text="单次发射上限:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['rf_guard_single_tx_limit_s'] = tk.StringVar(
            value=config.get('rf_guard_single_tx_limit_s', '30')
        )
        ttk.Entry(rf_guard_limit_row, textvariable=self.config_vars['rf_guard_single_tx_limit_s'], width=8).pack(side=tk.LEFT, padx=5)
        ttk.Label(rf_guard_limit_row, text="秒", foreground="gray").pack(side=tk.LEFT)

        rf_guard_window_row = ttk.Frame(edit_frame)
        rf_guard_window_row.pack(fill=tk.X, pady=2)
        ttk.Label(rf_guard_window_row, text="统计窗口:", width=14, anchor=tk.W).pack(side=tk.LEFT)
        self.config_vars['rf_guard_window_s'] = tk.StringVar(value=config.get('rf_guard_window_s', '300'))
        ttk.Entry(rf_guard_window_row, textvariable=self.config_vars['rf_guard_window_s'], width=8).pack(side=tk.LEFT, padx=5)
        ttk.Label(rf_guard_window_row, text="秒", foreground="gray").pack(side=tk.LEFT, padx=(0, 10))
        ttk.Label(rf_guard_window_row, text="累计上限:").pack(side=tk.LEFT)
        self.config_vars['rf_guard_max_tx_in_window_s'] = tk.StringVar(
            value=config.get('rf_guard_max_tx_in_window_s', '60')
        )
        ttk.Entry(rf_guard_window_row, textvariable=self.config_vars['rf_guard_max_tx_in_window_s'], width=8).pack(side=tk.LEFT, padx=5)
        ttk.Label(rf_guard_window_row, text="秒", foreground="gray").pack(side=tk.LEFT)

        # === 按钮区 ===
        btn_frame = ttk.Frame(frame)
        btn_frame.pack(fill=tk.X)

        def refresh_display():
            """刷新显示"""
            if self.client and hasattr(self, 'config_labels'):
                c = self.client.get_device_config()
                formatters = {
                    "rx_freq": lambda v: f"{int(v)/1e6:.4f} MHz" if v else "-",
                    "tx_freq": lambda v: f"{int(v)/1e6:.4f} MHz" if v else "-",
                    "rx_ctcss": lambda v: f"{float(v):.1f} Hz" if v and v != "0" and v != "0.0" else "关闭",
                    "tx_ctcss": lambda v: f"{float(v):.1f} Hz" if v and v != "0" and v != "0.0" else "关闭",
                    "rx_tone_mode": format_tone_mode,
                    "rx_tone_value": lambda v: v if v else "-",
                    "tx_tone_mode": format_tone_mode,
                    "tx_tone_value": lambda v: v if v else "-",
                    "sql_level": lambda v: f"{v}",
                    "power_level": lambda v: {"1": "低", "3": "高"}.get(v, v),
                    "tx_bandwidth": lambda v: "窄带" if v == "1" else "宽带",
                    "rf_guard_enabled": format_rf_guard_enabled,
                    "rf_guard_single_tx_limit_s": format_seconds,
                    "rf_guard_window_s": format_seconds,
                    "rf_guard_max_tx_in_window_s": format_seconds,
                }
                for key, label in self.config_labels.items():
                    if key in c:
                        try:
                            label.config(text=formatters.get(key, str)(c[key]))
                        except (ValueError, TypeError):
                            label.config(text=str(c[key]))
                self.log("[Config] 显示已刷新")

        def apply_local():
            """应用本地配置（更新本地存储，不上报）"""
            try:
                # 更新本地配置
                self.client.device_config = {
                    self.client._get_tlv_type('rx_freq'): str(int(float(self.config_vars['rx_freq'].get()) * 1e6)),
                    self.client._get_tlv_type('tx_freq'): str(int(float(self.config_vars['tx_freq'].get()) * 1e6)),
                    self.client._get_tlv_type('rx_ctcss'): self.config_vars['rx_ctcss'].get(),
                    self.client._get_tlv_type('tx_ctcss'): self.config_vars['tx_ctcss'].get(),
                    self.client._get_tlv_type('rx_tone_mode'): self.config_vars['rx_tone_mode'].get(),
                    self.client._get_tlv_type('rx_tone_value'): self.config_vars['rx_tone_value'].get(),
                    self.client._get_tlv_type('tx_tone_mode'): self.config_vars['tx_tone_mode'].get(),
                    self.client._get_tlv_type('tx_tone_value'): self.config_vars['tx_tone_value'].get(),
                    self.client._get_tlv_type('sql_level'): str(int(float(sql_scale.get()))),
                    self.client._get_tlv_type('power_level'): self.config_vars['power_level'].get(),
                    self.client._get_tlv_type('tx_bandwidth'): self.config_vars['tx_bandwidth'].get(),
                    self.client._get_tlv_type('rf_guard_enabled'): "1" if self.config_vars['rf_guard_enabled'].get() else "0",
                    self.client._get_tlv_type('rf_guard_single_tx_limit_s'): self.config_vars['rf_guard_single_tx_limit_s'].get(),
                    self.client._get_tlv_type('rf_guard_window_s'): self.config_vars['rf_guard_window_s'].get(),
                    self.client._get_tlv_type('rf_guard_max_tx_in_window_s'): self.config_vars['rf_guard_max_tx_in_window_s'].get(),
                }
                self.log("[Config] 本地配置已更新")
                refresh_display()
            except Exception as e:
                self.log(f"[Config] 应用失败: {e}")

        def report_to_server():
            """上报配置到服务器"""
            try:
                # 先更新本地配置
                apply_local()
                # 发送配置上报包
                self.client._send_config_report()
                self.log("[Config] 已上报到服务器")
            except Exception as e:
                self.log(f"[Config] 上报失败: {e}")

        ttk.Button(btn_frame, text="刷新显示", command=refresh_display).pack(side=tk.LEFT, padx=5)
        ttk.Button(btn_frame, text="应用本地", command=apply_local).pack(side=tk.LEFT, padx=5)
        ttk.Button(btn_frame, text="上报服务器", command=report_to_server).pack(side=tk.LEFT, padx=5)
        ttk.Button(btn_frame, text="关闭", command=config_window.destroy).pack(side=tk.RIGHT, padx=5)

        # 设置配置更新回调
        def on_config_update(new_config):
            self.app.root.after(0, refresh_display)

        self.client.config_update_callback = on_config_update

        # 窗口关闭时清除回调
        def on_close():
            if self.client:
                self.client.config_update_callback = None
            config_window.destroy()

        config_window.protocol("WM_DELETE_WINDOW", on_close)

    def show_dynamic_bind(self):
        """显示动态绑定测试窗口"""
        if self.client_type != "udp_device":
            return

        bind_window = tk.Toplevel(self.root)
        bind_window.title(f"动态绑定 - {self.panel_name}")
        bind_window.geometry("560x520")
        bind_window.resizable(False, False)

        outer = ttk.Frame(bind_window, padding=10)
        outer.pack(fill=tk.BOTH, expand=True)

        tips = (
            "流程: 设备预检查 -> 请求动态码 -> 用户登录并绑定 -> 提交SSID -> 设备确认绑定。\n"
            "完成后会自动回填当前面板的 username / device_password / ssid。"
        )
        ttk.Label(outer, text=tips, foreground="gray").pack(anchor=tk.W, pady=(0, 8))

        form = ttk.LabelFrame(outer, text="参数", padding=10)
        form.pack(fill=tk.X)

        bind_username_var = tk.StringVar(value=self.username_var.get() or "admin")
        bind_password_var = tk.StringVar(value="")
        bind_mac_var = tk.StringVar(value=self.mac_var.get() if hasattr(self, 'mac_var') else self._generate_random_mac())
        bind_device_password_var = tk.StringVar(value=self.password_var.get())
        bind_ssid_var = tk.StringVar(value=self.ssid_var.get() or "1")
        bind_code_var = tk.StringVar(value="")
        recommended_ssid_var = tk.StringVar(value="")
        available_ssids_var = tk.StringVar(value="")

        ttk.Label(form, text="账号用户名:").grid(row=0, column=0, sticky=tk.W, pady=2)
        ttk.Entry(form, textvariable=bind_username_var, width=16).grid(row=0, column=1, sticky=tk.W, padx=4, pady=2)

        ttk.Label(form, text="账号密码:").grid(row=0, column=2, sticky=tk.W, pady=2)
        ttk.Entry(form, textvariable=bind_password_var, width=16, show="*").grid(row=0, column=3, sticky=tk.W, padx=4, pady=2)

        ttk.Label(form, text="设备MAC:").grid(row=1, column=0, sticky=tk.W, pady=2)
        ttk.Entry(form, textvariable=bind_mac_var, width=18).grid(row=1, column=1, sticky=tk.W, padx=4, pady=2)
        ttk.Button(
            form,
            text="随机MAC",
            command=lambda: bind_mac_var.set(self._generate_random_mac())
        ).grid(row=1, column=2, sticky=tk.W, padx=4, pady=2)

        ttk.Label(form, text="现设备密码:").grid(row=2, column=0, sticky=tk.W, pady=2)
        ttk.Entry(form, textvariable=bind_device_password_var, width=16).grid(row=2, column=1, sticky=tk.W, padx=4, pady=2)

        ttk.Label(form, text="目标SSID:").grid(row=2, column=2, sticky=tk.W, pady=2)
        ttk.Entry(form, textvariable=bind_ssid_var, width=10).grid(row=2, column=3, sticky=tk.W, padx=4, pady=2)

        ttk.Label(form, text="动态码:").grid(row=3, column=0, sticky=tk.W, pady=2)
        ttk.Entry(form, textvariable=bind_code_var, width=16).grid(row=3, column=1, sticky=tk.W, padx=4, pady=2)

        ttk.Label(form, text="推荐SSID:").grid(row=3, column=2, sticky=tk.W, pady=2)
        ttk.Label(form, textvariable=recommended_ssid_var, foreground="blue").grid(row=3, column=3, sticky=tk.W, padx=4, pady=2)

        ttk.Label(form, text="可用SSID:").grid(row=4, column=0, sticky=tk.NW, pady=2)
        available_label = ttk.Label(form, textvariable=available_ssids_var, foreground="gray", wraplength=360, justify=tk.LEFT)
        available_label.grid(row=4, column=1, columnspan=3, sticky=tk.W, padx=4, pady=2)

        log_box = scrolledtext.ScrolledText(
            outer, width=66, height=16, wrap=tk.WORD,
            font=("Consolas", 9), state='disabled'
        )
        log_box.pack(fill=tk.BOTH, expand=True, pady=10)

        def bind_log(message: str):
            self.log(message)

            def append():
                log_box.config(state='normal')
                log_box.insert(tk.END, message + "\n")
                log_box.see(tk.END)
                log_box.config(state='disabled')

            self.app.root.after(0, append)

        def build_http_client() -> HTTPClient:
            server_ip = self.app.server_ip.get()
            http_port = self.app.http_port.get()
            return HTTPClient(f"http://{server_ip}:{http_port}", log_callback=bind_log)

        def parse_target_ssid() -> int | None:
            raw = bind_ssid_var.get().strip()
            if not raw:
                bind_log("[绑定错误] 请输入目标 SSID")
                return None
            try:
                return int(raw)
            except ValueError:
                bind_log("[绑定错误] 目标 SSID 必须是数字")
                return None

        def update_binding_hints(bind_result: dict):
            data = bind_result.get('data', {}) if isinstance(bind_result, dict) else {}
            recommended = data.get('recommended_ssid')
            available = data.get('available_ssids') or []
            recommended_ssid_var.set("" if recommended is None else str(recommended))
            available_ssids_var.set(", ".join(str(item) for item in available[:30]))
            if recommended is not None and not bind_ssid_var.get().strip():
                bind_ssid_var.set(str(recommended))

        def apply_ready_config(ready_result: dict):
            data = ready_result.get('data', {}) if isinstance(ready_result, dict) else {}
            username = data.get('username', '')
            device_password = data.get('device_password', '')
            ssid = data.get('ssid')
            dmr_id = data.get('dmr_id', 0)

            if username:
                self.username_var.set(username)
            if device_password:
                self.password_var.set(device_password)
                bind_device_password_var.set(device_password)
            if ssid is not None:
                self.ssid_var.set(str(ssid))
                bind_ssid_var.set(str(ssid))
            if hasattr(self, 'mac_var'):
                self.mac_var.set(bind_mac_var.get().strip())
            self.bound_dmrid = dmr_id or 0
            bind_log(
                f"[绑定完成] 已回填 username={username}, ssid={ssid}, dmr_id={self.bound_dmrid}"
            )

        def do_pre_check():
            http = build_http_client()
            result = http.device_pre_check(
                bind_mac_var.get().strip(),
                bind_username_var.get().strip(),
                bind_device_password_var.get().strip(),
            )
            bind_log(f"[预检查响应] {result}")
            return result

        def do_request_code():
            http = build_http_client()
            result = http.request_device_code(bind_mac_var.get().strip())
            if result.get('code') == 200:
                code = result.get('data', {}).get('dynamic_code', '')
                if code:
                    bind_code_var.set(code)
            bind_log(f"[请求动态码响应] {result}")
            return result

        def do_bind():
            username = bind_username_var.get().strip()
            password = bind_password_var.get().strip()
            code = bind_code_var.get().strip()
            if not username or not password:
                bind_log("[绑定错误] 请输入账号用户名和密码")
                return None
            if not code:
                bind_log("[绑定错误] 请先获取动态码")
                return None

            http = build_http_client()
            if not http.login(username, password):
                return None

            result = http.bind_device(code)
            update_binding_hints(result)
            bind_log(f"[设备绑定响应] {result}")
            return result

        def do_submit():
            username = bind_username_var.get().strip()
            password = bind_password_var.get().strip()
            target_ssid = parse_target_ssid()
            if not username or not password:
                bind_log("[提交错误] 请输入账号用户名和密码")
                return None
            if target_ssid is None:
                return None

            http = build_http_client()
            if not http.login(username, password):
                return None

            result = http.submit_device_config(bind_mac_var.get().strip(), target_ssid)
            bind_log(f"[提交配置响应] {result}")
            return result

        def do_confirm(apply_to_panel: bool = False):
            http = build_http_client()
            result = http.confirm_device_bind(bind_mac_var.get().strip())
            bind_log(f"[确认绑定响应] {result}")
            if apply_to_panel and result.get('code') == 200 and result.get('data', {}).get('status') == 'ready':
                apply_ready_config(result)
            return result

        def do_full_bind():
            pre_check = do_pre_check()
            if not isinstance(pre_check, dict):
                return

            status = pre_check.get('data', {}).get('status', '')
            if status == 'authenticated':
                bind_log("[流程] 当前参数已经可直接认证，无需动态绑定")
                return

            code_result = do_request_code()
            if not isinstance(code_result, dict) or code_result.get('code') != 200:
                return

            bind_result = do_bind()
            if not isinstance(bind_result, dict) or bind_result.get('code') != 200:
                return

            submit_result = do_submit()
            if not isinstance(submit_result, dict) or submit_result.get('code') != 200:
                return

            do_confirm(apply_to_panel=True)

        btn_frame = ttk.Frame(outer)
        btn_frame.pack(fill=tk.X)

        ttk.Button(btn_frame, text="1. 预检查", command=lambda: threading.Thread(target=do_pre_check, daemon=True).start()).pack(side=tk.LEFT, padx=4)
        ttk.Button(btn_frame, text="2. 取动态码", command=lambda: threading.Thread(target=do_request_code, daemon=True).start()).pack(side=tk.LEFT, padx=4)
        ttk.Button(btn_frame, text="3. 用户绑定", command=lambda: threading.Thread(target=do_bind, daemon=True).start()).pack(side=tk.LEFT, padx=4)
        ttk.Button(btn_frame, text="4. 提交配置", command=lambda: threading.Thread(target=do_submit, daemon=True).start()).pack(side=tk.LEFT, padx=4)
        ttk.Button(btn_frame, text="5. 设备确认", command=lambda: threading.Thread(target=lambda: do_confirm(apply_to_panel=True), daemon=True).start()).pack(side=tk.LEFT, padx=4)

        bottom_btn_frame = ttk.Frame(outer)
        bottom_btn_frame.pack(fill=tk.X, pady=(8, 0))
        ttk.Button(bottom_btn_frame, text="一键完整流程", command=lambda: threading.Thread(target=do_full_bind, daemon=True).start()).pack(side=tk.LEFT, padx=4)
        ttk.Button(bottom_btn_frame, text="同步到面板MAC", command=lambda: self.mac_var.set(bind_mac_var.get().strip())).pack(side=tk.LEFT, padx=4)
        ttk.Button(bottom_btn_frame, text="关闭", command=bind_window.destroy).pack(side=tk.RIGHT, padx=4)

    def stop(self):
        """停止客户端"""
        try:
            if self.client:
                self.client.disconnect()
        except Exception as e:
            print(f"[Stop] 断开连接异常: {e}")
        self.client = None


class DebugClientApp:
    """主应用程序"""

    def __init__(self, root):
        self.root = root
        self.root.title("DraARL 调试客户端")
        self.root.geometry("1300x500")
        self.root.minsize(1200, 400)
        self.root.protocol("WM_DELETE_WINDOW", self.on_closing)

        self.panels = []
        self._build_ui()

    def _build_ui(self):
        """构建 UI"""
        # 顶部：服务器配置
        server_frame = ttk.LabelFrame(self.root, text="服务器配置", padding=(10, 5))
        server_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(server_frame, text="服务器IP:").grid(row=0, column=0, sticky=tk.W)
        self.server_ip = tk.StringVar(value="ptt.4l2.cn")
        ttk.Entry(server_frame, textvariable=self.server_ip, width=12).grid(row=0, column=1, padx=5)

        ttk.Label(server_frame, text="UDP端口:").grid(row=0, column=2, sticky=tk.W, padx=(10,0))
        self.udp_port = tk.StringVar(value="60050")
        ttk.Entry(server_frame, textvariable=self.udp_port, width=6).grid(row=0, column=3, padx=5)

        ttk.Label(server_frame, text="HTTP端口:").grid(row=0, column=4, sticky=tk.W, padx=(10,0))
        self.http_port = tk.StringVar(value="9002")
        ttk.Entry(server_frame, textvariable=self.http_port, width=6).grid(row=0, column=5, padx=5)

        ttk.Button(server_frame, text="全部连接", command=self.connect_all).grid(row=0, column=6, padx=10)
        ttk.Button(server_frame, text="全部断开", command=self.disconnect_all).grid(row=0, column=7, padx=5)

        # 中部：三个客户端面板
        panels_frame = ttk.Frame(self.root)
        panels_frame.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)

        self.udp_device_panel = ClientPanel(panels_frame, "UDP 普通设备", "udp_device", self)
        self.udp_device_panel.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=(0, 5))

        self.udp_jwt_panel = ClientPanel(panels_frame, "UDP JWT", "udp_jwt", self)
        self.udp_jwt_panel.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=5)

        self.serial_panel = ClientPanel(panels_frame, "COM7 串口", "serial_device", self)
        self.serial_panel.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=(5, 0))

        self.panels = [
            self.udp_device_panel,
            self.udp_jwt_panel,
            self.serial_panel,
        ]

        # 底部：快捷键说明
        help_frame = ttk.LabelFrame(self.root, text="说明", padding=(10, 5))
        help_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(help_frame, text="UDP普通设备: SSID 1-99/106-254, 使用设备密码认证，可打开动态绑定助手 (快捷键: 1 发言)").pack(anchor=tk.W)
        ttk.Label(help_frame, text="UDP JWT: DevModel 101-104, SSID=DevModel, 使用Token认证 (快捷键: 2 发言)").pack(anchor=tk.W)
        ttk.Label(help_frame, text="COM7 串口: 通过串口直接发送数据包到4G透传模块, 波特率 921600 (快捷键: 3 发言)").pack(anchor=tk.W)

        # 绑定快捷键
        self.root.bind("<KeyPress-1>", lambda e: self.udp_device_panel.on_ptt_press())
        self.root.bind("<KeyRelease-1>", lambda e: self.udp_device_panel.on_ptt_release())
        self.root.bind("<KeyPress-2>", lambda e: self.udp_jwt_panel.on_ptt_press())
        self.root.bind("<KeyRelease-2>", lambda e: self.udp_jwt_panel.on_ptt_release())
        self.root.bind("<KeyPress-3>", lambda e: self.serial_panel.on_ptt_press())
        self.root.bind("<KeyRelease-3>", lambda e: self.serial_panel.on_ptt_release())

    def connect_all(self):
        """连接所有"""
        for panel in self.panels:
            if not panel.is_connected:
                panel.toggle_connection()

    def disconnect_all(self):
        """断开所有"""
        for panel in self.panels:
            if panel.is_connected:
                panel.toggle_connection()

    def on_closing(self):
        """关闭窗口"""
        for panel in self.panels:
            panel.stop()
        self.root.destroy()


def main():
    """主函数"""
    root = tk.Tk()
    app = DebugClientApp(root)
    root.focus_set()
    root.mainloop()


if __name__ == "__main__":
    main()
