"""
Python 模拟客户端的 JWT Token 生成工具
"""

import os
import time
import hmac
import hashlib
import base64
import json
from typing import Optional, List


# Never ship a signing secret in the simulator.  The service rejects the old
# public development key, so test callers must provide the key configured for
# their isolated test instance explicitly or through this environment value.
TEST_SECRET_ENV = "DRAARL_TEST_JWT_SECRET"
MIN_SECRET_LENGTH = 32


def resolve_test_secret(secret: Optional[str] = None) -> str:
    """Return an explicit test signing key and reject unsafe implicit values."""
    value = (secret if secret is not None else os.getenv(TEST_SECRET_ENV, "")).strip()
    if not value:
        raise ValueError(
            f"JWT test key is required; pass secret=... or set {TEST_SECRET_ENV}"
        )
    if len(value) < MIN_SECRET_LENGTH:
        raise ValueError(
            f"JWT test key must be at least {MIN_SECRET_LENGTH} characters"
        )
    return value


def base64url_encode(data: bytes) -> str:
    """Base64 URL 安全编码"""
    return base64.urlsafe_b64encode(data).rstrip(b'=').decode('ascii')


def base64url_decode(data: str) -> bytes:
    """Base64 URL 安全解码"""
    padding = 4 - len(data) % 4
    if padding != 4:
        data += '=' * padding
    return base64.urlsafe_b64decode(data)


def generate_jwt(
    username: str,
    roles: Optional[List[str]] = None,
    secret: Optional[str] = None,
    expire_days: int = 30
) -> str:
    """
    生成 JWT Token

    Args:
        username: 用户名
        roles: 角色列表
        secret: JWT 密钥
        expire_days: 过期天数

    Returns:
        JWT Token 字符串
    """
    if not username.strip():
        raise ValueError("JWT username is required")
    secret = resolve_test_secret(secret)
    if roles is None:
        roles = ["user"]

    now = int(time.time())
    expire = now + expire_days * 24 * 60 * 60

    # Header
    header = {
        "alg": "HS256",
        "typ": "JWT"
    }

    # Payload
    payload = {
        "username": username,
        "roles": roles,
        "exp": expire,
        "iat": now,
        "iss": "draarl"
    }

    # 编码 Header 和 Payload
    header_b64 = base64url_encode(json.dumps(header, separators=(',', ':')).encode())
    payload_b64 = base64url_encode(json.dumps(payload, separators=(',', ':')).encode())

    # 签名
    message = f"{header_b64}.{payload_b64}"
    signature = hmac.new(
        secret.encode(),
        message.encode(),
        hashlib.sha256
    ).digest()
    signature_b64 = base64url_encode(signature)

    return f"{header_b64}.{payload_b64}.{signature_b64}"


def parse_jwt(token: str, secret: Optional[str] = None) -> Optional[dict]:
    """
    解析并验证 JWT Token

    Args:
        token: JWT Token 字符串
        secret: JWT 密钥

    Returns:
        解析后的 Payload，验证失败返回 None
    """
    try:
        secret = resolve_test_secret(secret)
        parts = token.split('.')
        if len(parts) != 3:
            return None

        header_b64, payload_b64, signature_b64 = parts

        # 验证签名
        message = f"{header_b64}.{payload_b64}"
        expected_sig = hmac.new(
            secret.encode(),
            message.encode(),
            hashlib.sha256
        ).digest()
        expected_sig_b64 = base64url_encode(expected_sig)

        if signature_b64 != expected_sig_b64:
            return None

        # 解析 Payload
        payload = json.loads(base64url_decode(payload_b64).decode())

        # 检查过期时间
        if payload.get('exp', 0) < time.time():
            return None

        return payload

    except Exception:
        return None


def get_username_from_token(token: str, secret: Optional[str] = None) -> Optional[str]:
    """从 Token 获取用户名"""
    payload = parse_jwt(token, secret)
    if payload:
        return payload.get('username')
    return None


if __name__ == "__main__":
    # Manual smoke test: DRAARL_TEST_JWT_SECRET must be set explicitly.
    token = generate_jwt("admin", ["admin", "user"])
    print(f"Token: {token}")
    print(f"Payload: {parse_jwt(token)}")
