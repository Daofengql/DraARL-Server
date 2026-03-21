"""
Nrllink 调试客户端
支持 UDP普通设备、UDP JWT 两种连接方式
"""

import tkinter as tk
from tkinter import ttk, scrolledtext, messagebox
import threading
import sys
import os

# 添加当前目录到路径
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from client.udp_device import UDPDeviceClient
from client.udp_jwt import UDPJWTClient
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
        self.password_var = tk.StringVar(value="FdWisUYY")
        ttk.Entry(parent, textvariable=self.password_var, width=10).grid(row=0, column=3, padx=2)

        ttk.Label(parent, text="SSID:").grid(row=1, column=0, sticky=tk.W)
        self.ssid_var = tk.StringVar(value="1")
        ttk.Entry(parent, textvariable=self.ssid_var, width=5).grid(row=1, column=1, padx=2, sticky=tk.W)

        ttk.Label(parent, text="型号:").grid(row=1, column=2, sticky=tk.W, padx=(5,0))
        self.devmodel_var = tk.StringVar(value="107")
        devmodel_combo = ttk.Combobox(parent, textvariable=self.devmodel_var, width=10,
                                       values=["100", "106", "107"])
        devmodel_combo.grid(row=1, column=3, padx=2)

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

    def stop(self):
        """停止客户端"""
        if self.client:
            self.client.disconnect()


class DebugClientApp:
    """主应用程序"""

    def __init__(self, root):
        self.root = root
        self.root.title("Nrllink 调试客户端")
        self.root.geometry("900x500")
        self.root.minsize(800, 400)
        self.root.protocol("WM_DELETE_WINDOW", self.on_closing)

        self.panels = []
        self._build_ui()

    def _build_ui(self):
        """构建 UI"""
        # 顶部：服务器配置
        server_frame = ttk.LabelFrame(self.root, text="服务器配置", padding=(10, 5))
        server_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(server_frame, text="服务器IP:").grid(row=0, column=0, sticky=tk.W)
        self.server_ip = tk.StringVar(value="127.0.0.1")
        ttk.Entry(server_frame, textvariable=self.server_ip, width=12).grid(row=0, column=1, padx=5)

        ttk.Label(server_frame, text="UDP端口:").grid(row=0, column=2, sticky=tk.W, padx=(10,0))
        self.udp_port = tk.StringVar(value="60050")
        ttk.Entry(server_frame, textvariable=self.udp_port, width=6).grid(row=0, column=3, padx=5)

        ttk.Label(server_frame, text="HTTP端口:").grid(row=0, column=4, sticky=tk.W, padx=(10,0))
        self.http_port = tk.StringVar(value="9002")
        ttk.Entry(server_frame, textvariable=self.http_port, width=6).grid(row=0, column=5, padx=5)

        ttk.Button(server_frame, text="全部连接", command=self.connect_all).grid(row=0, column=6, padx=10)
        ttk.Button(server_frame, text="全部断开", command=self.disconnect_all).grid(row=0, column=7, padx=5)

        # 群组切换区（第二行）
        ttk.Label(server_frame, text="群组ID:").grid(row=1, column=0, sticky=tk.W, pady=(5,0))
        self.group_id = tk.StringVar(value="999")
        ttk.Entry(server_frame, textvariable=self.group_id, width=6).grid(row=1, column=1, padx=5, pady=(5,0), sticky=tk.W)

        ttk.Button(server_frame, text="切换群组(JWT)", command=self.switch_group_jwt).grid(row=1, column=2, padx=5, pady=(5,0))
        ttk.Button(server_frame, text="刷新群组列表", command=self.refresh_groups).grid(row=1, column=3, padx=5, pady=(5,0))

        # HTTP 客户端（用于群组切换等 API 调用）
        self.http_client = None

        # 中部：两个客户端面板
        panels_frame = ttk.Frame(self.root)
        panels_frame.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)

        self.udp_device_panel = ClientPanel(panels_frame, "UDP 普通设备", "udp_device", self)
        self.udp_device_panel.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=(0, 5))

        self.udp_jwt_panel = ClientPanel(panels_frame, "UDP JWT", "udp_jwt", self)
        self.udp_jwt_panel.pack(side=tk.LEFT, fill=tk.BOTH, expand=True, padx=(5, 0))

        self.panels = [
            self.udp_device_panel,
            self.udp_jwt_panel,
        ]

        # 底部：快捷键说明
        help_frame = ttk.LabelFrame(self.root, text="说明", padding=(10, 5))
        help_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(help_frame, text="UDP普通设备: SSID 1-99/106-235, 使用设备密码认证 (快捷键: 1)").pack(anchor=tk.W)
        ttk.Label(help_frame, text="UDP JWT: DevModel 101-104, SSID=DevModel, 使用Token认证 (快捷键: 2)").pack(anchor=tk.W)
        ttk.Label(help_frame, text="WebSocket JWT: 请通过前端浏览器测试").pack(anchor=tk.W)

        # 绑定快捷键
        self.root.bind("<KeyPress-1>", lambda e: self.udp_device_panel.on_ptt_press())
        self.root.bind("<KeyRelease-1>", lambda e: self.udp_device_panel.on_ptt_release())
        self.root.bind("<KeyPress-2>", lambda e: self.udp_jwt_panel.on_ptt_press())
        self.root.bind("<KeyRelease-2>", lambda e: self.udp_jwt_panel.on_ptt_release())

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

    def _get_http_client(self) -> HTTPClient:
        """获取或创建 HTTP 客户端"""
        if not self.http_client:
            server_ip = self.server_ip.get()
            http_port = self.http_port.get()
            self.http_client = HTTPClient(f"http://{server_ip}:{http_port}")
        return self.http_client

    def switch_group_jwt(self):
        """切换 JWT 客户端的群组（通过 HTTP API）"""
        try:
            group_id = int(self.group_id.get())
        except ValueError:
            messagebox.showerror("错误", "群组 ID 必须是数字")
            return

        client = self._get_http_client()

        # 需要先登录或设置 Token
        # 尝试从 JWT 面板获取 Token
        token = None
        if self.udp_jwt_panel.is_connected and self.udp_jwt_panel.client:
            token = getattr(self.udp_jwt_panel.client, 'jwt_token', None)

        if not token:
            # 使用默认 Token
            token = generate_jwt("admin", ["user"])

        client.set_token(token)

        # 调用切换群组 API
        success = client.update_radio_group(group_id, dev_model=103)
        if success:
            messagebox.showinfo("成功", f"已切换到群组 {group_id}")
        else:
            messagebox.showerror("失败", "切换群组失败，请查看日志")

    def refresh_groups(self):
        """刷新群组列表"""
        client = self._get_http_client()

        # 使用默认 Token
        token = generate_jwt("admin", ["user"])
        client.set_token(token)

        groups = client.get_groups()
        if groups:
            group_info = "\n".join([
                f"  {g.get('id')}: {g.get('name', 'N/A')} ({g.get('device_count', 0)} 设备)"
                for g in groups[:10]  # 只显示前 10 个
            ])
            messagebox.showinfo("群组列表", f"群组:\n{group_info}")
        else:
            messagebox.showwarning("提示", "未获取到群组或需要登录")

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
