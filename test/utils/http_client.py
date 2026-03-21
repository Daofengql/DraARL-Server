"""
HTTP API 客户端
用于群组切换等操作
"""

import requests
from typing import Optional, Dict, Any


class HTTPClient:
    """HTTP API 客户端"""

    def __init__(self, base_url: str, log_callback=None):
        """
        初始化 HTTP 客户端

        Args:
            base_url: 基础 URL，如 http://127.0.0.1:60050
            log_callback: 日志回调函数
        """
        self.base_url = base_url.rstrip('/')
        self.log_callback = log_callback or print
        self.token = None
        self.session = requests.Session()

    def log(self, message: str):
        """输出日志"""
        if self.log_callback:
            self.log_callback(message)

    def set_token(self, token: str):
        """设置 JWT Token"""
        self.token = token
        if token:
            self.session.headers.update({
                'Authorization': f'Bearer {token}'
            })

    def _request(self, method: str, path: str, **kwargs) -> Dict[str, Any]:
        """发送请求"""
        url = f"{self.base_url}{path}"
        try:
            response = self.session.request(method, url, **kwargs)
            return response.json()
        except Exception as e:
            self.log(f"[HTTP错误] {e}")
            return {"code": 500, "message": str(e)}

    # ==========================================
    # 认证相关
    # ==========================================

    def login(self, username: str, password: str) -> bool:
        """
        用户登录

        Args:
            username: 用户名
            password: 密码

        Returns:
            是否登录成功
        """
        result = self._request('POST', '/api/auth/login', json={
            'username': username,
            'password': password
        })

        if result.get('code') == 200:
            data = result.get('data', {})
            self.token = data.get('token')
            if self.token:
                self.session.headers.update({
                    'Authorization': f'Bearer {self.token}'
                })
            self.log(f"[登录成功] {username}")
            return True
        else:
            self.log(f"[登录失败] {result.get('message', '未知错误')}")
            return False

    # ==========================================
    # 群组相关
    # ==========================================

    def get_groups(self) -> list:
        """
        获取群组列表

        Returns:
            群组列表
        """
        result = self._request('GET', '/api/groups')
        if result.get('code') == 200:
            return result.get('data', [])
        return []

    def get_group(self, group_id: int) -> Optional[Dict]:
        """
        获取群组详情

        Args:
            group_id: 群组 ID

        Returns:
            群组信息
        """
        result = self._request('GET', f'/api/groups/{group_id}')
        if result.get('code') == 200:
            return result.get('data')
        return None

    def change_device_group(self, device_id: int, group_id: int, password: str = "") -> bool:
        """
        切换普通设备群组

        Args:
            device_id: 设备 ID
            group_id: 目标群组 ID
            password: 群组密码（私有群组需要）

        Returns:
            是否切换成功
        """
        result = self._request('POST', '/api/device/changegroup', json={
            'device_id': device_id,
            'group_id': group_id,
            'password': password
        })

        if result.get('code') == 200:
            self.log(f"[群组切换成功] 设备 {device_id} -> 群组 {group_id}")
            return True
        else:
            self.log(f"[群组切换失败] {result.get('message', '未知错误')}")
            return False

    def update_radio_group(self, group_id: int, dev_model: int = 105) -> bool:
        """
        切换幽灵设备群组（JWT 认证的 App/Web 客户端）

        Args:
            group_id: 目标群组 ID
            dev_model: 设备型号 (101=Android, 102=iOS, 103=Windows, 104=macOS, 105=Web)

        Returns:
            是否切换成功
        """
        result = self._request('PUT', '/api/radio/group', json={
            'group_id': group_id,
            'dev_model': dev_model
        })

        if result.get('code') == 200:
            self.log(f"[群组切换成功] -> 群组 {group_id}")
            return True
        else:
            self.log(f"[群组切换失败] {result.get('message', '未知错误')}")
            return False

    def join_group(self, group_id: int, password: str = "") -> bool:
        """
        加入群组

        Args:
            group_id: 群组 ID
            password: 群组密码（私有群组需要）

        Returns:
            是否加入成功
        """
        result = self._request('POST', f'/api/groups/{group_id}/join', json={
            'password': password
        })

        if result.get('code') == 200:
            self.log(f"[加入群组成功] 群组 {group_id}")
            return True
        else:
            self.log(f"[加入群组失败] {result.get('message', '未知错误')}")
            return False

    def leave_group(self, group_id: int) -> bool:
        """
        离开群组

        Args:
            group_id: 群组 ID

        Returns:
            是否离开成功
        """
        result = self._request('POST', f'/api/groups/{group_id}/leave')

        if result.get('code') == 200:
            self.log(f"[离开群组成功] 群组 {group_id}")
            return True
        else:
            self.log(f"[离开群组失败] {result.get('message', '未知错误')}")
            return False

    # ==========================================
    # 设备相关
    # ==========================================

    def get_devices(self) -> list:
        """
        获取设备列表

        Returns:
            设备列表
        """
        result = self._request('GET', '/api/devices')
        if result.get('code') == 200:
            return result.get('data', [])
        return []

    def get_radio_status(self) -> Optional[Dict]:
        """
        获取幽灵设备状态

        Returns:
            设备状态
        """
        result = self._request('GET', '/api/radio/status')
        if result.get('code') == 200:
            return result.get('data')
        return None
