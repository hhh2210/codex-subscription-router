import importlib.util
import plistlib
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch


spec = importlib.util.spec_from_file_location("patch_app", Path(__file__).with_name("patch_app.py"))
patch_app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(patch_app)


class RuntimeEntitlementsTests(unittest.TestCase):
    def test_removes_push_and_team_grants_but_preserves_runtime_capabilities(self):
        original = {
            "aps-environment": "production",
            "com.apple.developer.aps-environment": "production",
            "com.apple.developer.team-identifier": "EXAMPLETEAM",
            "keychain-access-groups": ["EXAMPLETEAM.app"],
            "com.apple.security.cs.allow-jit": True,
            "com.apple.security.network.client": True,
        }
        result = subprocess.CompletedProcess([], 0, stdout=plistlib.dumps(original))
        with patch.object(patch_app.subprocess, "run", return_value=result):
            actual = patch_app.sanitized_runtime_entitlements(Path("test-runtime"))
        self.assertEqual(actual, {
            "com.apple.security.cs.allow-jit": True,
            "com.apple.security.network.client": True,
        })


if __name__ == "__main__":
    unittest.main()
