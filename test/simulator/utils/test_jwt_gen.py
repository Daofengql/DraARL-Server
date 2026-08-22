"""Regression tests for explicit-key JWT test helpers."""

import os
import unittest

try:
    from .jwt_gen import generate_jwt, get_username_from_token, parse_jwt
except ImportError:  # unittest discover -s test/simulator/utils
    from jwt_gen import generate_jwt, get_username_from_token, parse_jwt


TEST_SECRET = "simulator-test-secret-0123456789abcdef"


class JWTGeneratorTest(unittest.TestCase):
    def setUp(self):
        self.previous = os.environ.pop("DRAARL_TEST_JWT_SECRET", None)

    def tearDown(self):
        if self.previous is not None:
            os.environ["DRAARL_TEST_JWT_SECRET"] = self.previous
        else:
            os.environ.pop("DRAARL_TEST_JWT_SECRET", None)

    def test_requires_explicit_key(self):
        with self.assertRaises(ValueError):
            generate_jwt("tester")

    def test_rejects_short_key(self):
        with self.assertRaises(ValueError):
            generate_jwt("tester", secret="too-short")

    def test_round_trip_with_explicit_key(self):
        token = generate_jwt("tester", ["user"], secret=TEST_SECRET)
        claims = parse_jwt(token, TEST_SECRET)
        self.assertIsNotNone(claims)
        self.assertEqual(claims["username"], "tester")
        self.assertEqual(get_username_from_token(token, TEST_SECRET), "tester")

    def test_environment_key_is_supported(self):
        os.environ["DRAARL_TEST_JWT_SECRET"] = TEST_SECRET
        token = generate_jwt("tester")
        self.assertEqual(get_username_from_token(token), "tester")


if __name__ == "__main__":
    unittest.main()
