"""Regression tests for direct test-key HTTP authentication."""

import unittest

try:
    from .http_client import HTTPClient
    from .jwt_gen import parse_jwt
except ImportError:  # unittest discover -s test/simulator/utils
    from http_client import HTTPClient
    from jwt_gen import parse_jwt


TEST_SECRET = "simulator-http-test-secret-0123456789"


class HTTPClientAuthTest(unittest.TestCase):
    def test_authenticate_with_test_key_sets_bearer_header(self):
        client = HTTPClient("http://127.0.0.1:1")
        token = client.authenticate_with_test_key("tester", ["admin"], TEST_SECRET)
        self.assertEqual(client.session.headers["Authorization"], f"Bearer {token}")
        self.assertEqual(parse_jwt(token, TEST_SECRET)["roles"], ["admin"])


if __name__ == "__main__":
    unittest.main()
