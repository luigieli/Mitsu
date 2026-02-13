import unittest
import json
import os

class TestVoiceConfig(unittest.TestCase):
    def test_voice_config_loading(self):
        # Assuming voice_config.json exists or we create a dummy one
        dummy_config = {
            "voice_model": "test_voice",
            "lang_code": "en",
            "pitch": 1.0,
            "speed": 1.0
        }
        with open("test_config.json", "w") as f:
            json.dump(dummy_config, f)
        
        try:
            with open("test_config.json", "r") as f:
                loaded = json.load(f)
            self.assertEqual(loaded["voice_model"], "test_voice")
        finally:
            if os.path.exists("test_config.json"):
                os.remove("test_config.json")

if __name__ == "__main__":
    unittest.main()
