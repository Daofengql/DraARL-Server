"""
基础客户端类
"""

import threading
import time
from abc import ABC, abstractmethod
from typing import Callable, Optional
from enum import Enum


class ClientState(Enum):
    """客户端状态"""
    DISCONNECTED = "已断开"
    CONNECTING = "连接中"
    AUTHENTICATING = "认证中"
    CONNECTED = "已连接"
    ERROR = "错误"


class BaseClient(ABC):
    """基础客户端抽象类"""

    def __init__(self, log_callback: Optional[Callable[[str], None]] = None):
        self.log_callback = log_callback or print
        self.state = ClientState.DISCONNECTED
        self.running = False
        self.is_transmitting = False
        self._threads: list = []

        # 认证结果
        self.authenticated = False
        self.callsign = ""
        self.ssid = 0

    def log(self, message: str):
        """线程安全的日志输出"""
        if self.log_callback:
            self.log_callback(message)

    def _start_thread(self, target: callable, name: str, daemon: bool = True) -> threading.Thread:
        """启动并管理线程"""
        t = threading.Thread(target=target, name=name, daemon=daemon)
        t.start()
        self._threads.append(t)
        return t

    @abstractmethod
    def connect(self) -> bool:
        """连接服务器"""
        pass

    @abstractmethod
    def disconnect(self):
        """断开连接"""
        pass

    @abstractmethod
    def send_heartbeat(self):
        """发送心跳"""
        pass

    @abstractmethod
    def send_text(self, text: str):
        """发送文本消息"""
        pass

    def start_transmit(self):
        """开始发射（PTT按下）"""
        if self.authenticated:
            self.is_transmitting = True
            self.log(">>> [开始发射]")

    def stop_transmit(self):
        """停止发射（PTT释放）"""
        self.is_transmitting = False
        self.log("<<< [停止发射]")

    def _set_state(self, state: ClientState):
        """设置状态"""
        self.state = state
        self.log(f"[状态] {state.value}")
