import torch
import os
import sys

# This script is intended to run inside the Kokoro container or where torch is available
# Default VOICE_DIR inside the kokoro-fastapi container
VOICE_DIR = "/app/api/src/voices/v1_0"

# If running locally for testing, we might want to override VOICE_DIR
if len(sys.argv) > 1:
    VOICE_DIR = sys.argv[1]

def blend_voices():
    try:
        print(f"Loading base voices from {VOICE_DIR}...")
        dora = torch.load(os.path.join(VOICE_DIR, "pf_dora.pt"), weights_only=True, map_location="cpu")
        heart = torch.load(os.path.join(VOICE_DIR, "af_heart.pt"), weights_only=True, map_location="cpu")
        alpha = torch.load(os.path.join(VOICE_DIR, "jf_alpha.pt"), weights_only=True, map_location="cpu")

        # --- RECIPE 1: ANIME PORTUGUESE (Mitsu-PT) ---
        # 60% Dora (Pronunciation) + 40% Alpha (Anime Tone)
        print("Blending Mitsu Anime PT (60% Dora, 40% Alpha)...")
        mitsu_pt = (dora * 0.7) + (alpha * 0.3)
        torch.save(mitsu_pt, os.path.join(VOICE_DIR, "mitsu_anime_pt.pt"))

        # --- RECIPE 2: ANIME ENGLISH (Mitsu-EN) ---
        # 50% Heart (Soft/Breathy) + 50% Alpha (Anime Tone)
        print("Blending Mitsu Anime EN (50% Heart, 50% Alpha)...")
        mitsu_en = (heart * 0.5) + (alpha * 0.5)
        torch.save(mitsu_en, os.path.join(VOICE_DIR, "mitsu_anime_en.pt"))

        print("Successfully created anime voices: mitsu_anime_pt.pt and mitsu_anime_en.pt")
    except Exception as e:
        print(f"Error during blending: {e}")
        sys.exit(1)

if __name__ == "__main__":
    blend_voices()
