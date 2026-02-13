import torch
import os

VOICE_DIR = "/app/api/src/voices/v1_0"
DORA = torch.load(os.path.join(VOICE_DIR, "pf_dora.pt"), map_location="cpu")
BELLA = torch.load(os.path.join(VOICE_DIR, "af_bella.pt"), map_location="cpu")
NEZUMI = torch.load(os.path.join(VOICE_DIR, "jf_nezumi.pt"), map_location="cpu")

OUTPUT_PATH = os.path.join(VOICE_DIR, "mitsu_custom.pt")

def create_anime_balanced_v2():
    print("Creating Balanced Anime Blend V2 (60% Nezumi, 30% Dora, 10% Bella)...")
    
    # 60% Nezumi (JP Anime)
    # 30% Dora (BR Native)
    # 10% Bella (US Soft)
    blended = (0.60 * NEZUMI) + (0.30 * DORA) + (0.10 * BELLA)
    
    torch.save(blended, OUTPUT_PATH)
    print(f"Balanced V2 voice saved to {OUTPUT_PATH}")

if __name__ == "__main__":
    create_anime_balanced_v2()
