import requests
import subprocess
import json
import sys

# CONFIGURATION
KOKORO_URL = "http://localhost:8880/v1/audio/speech"

# Filter chains: Removed boxiness (400Hz) and boosted clarity (3kHz/8kHz)
FILTERS_PT = (
    "highpass=f=150,"             # Balance between warmth and mud
    "equalizer=f=400:t=q:w=1:g=-5," # CUT THE BOX: 400Hz is the 'inside a box' frequency
    "equalizer=f=3000:t=q:w=1:g=3," # CLARITY: Boost presence for speech definition
    "equalizer=f=8000:t=q:w=1:g=6," # AIR: Boost sparkle
    "asetrate=24000*1.19,"
    "aresample=24000,"
    "compand=attacks=0:points=-80/-80|-20/-12|0/-3,"
    "volume=1.25,"
    "aecho=0.8:0.3:15:0.02"
)

FILTERS_EN = (
    "highpass=f=120,"
    "equalizer=f=400:t=q:w=1:g=-3,"
    "equalizer=f=3000:t=q:w=1:g=2,"
    "equalizer=f=8000:t=q:w=1:g=4,"
    "asetrate=24000*1.12,"
    "aresample=24000,"
    "compand=attacks=0:points=-80/-80|-20/-12|0/-3,"
    "volume=1.25,"
    "aecho=0.8:0.3:15:0.02"
)

def test_voice(text, lang="pt"):
    print(f"--- Testing {lang.upper()} Voice ---")
    print(f"Input: {text}")
    
    voice = "mitsu_custom" if lang == "pt" else "af_heart"
    lang_code = "p" if lang == "pt" else "a"
    filters = FILTERS_PT if lang == "pt" else FILTERS_EN

    payload = {
        "input": text,
        "voice": voice,
        "lang_code": lang_code,
        "speed": 1.2,
        "model": "kokoro"
    }

    print("Requesting audio from Kokoro...")
    try:
        response = requests.post(KOKORO_URL, json=payload, stream=True)
        response.raise_for_status()
    except Exception as e:
        print(f"Error connecting to Kokoro: {e}")
        return

    print("Playing via FFmpeg + paplay...")
    # This pipes the Kokoro raw output into FFmpeg, then into paplay for instant hearing
    ffmpeg_cmd = [
        "ffmpeg", "-i", "pipe:0",
        "-af", filters,
        "-f", "wav", "pipe:1"
    ]
    
    paplay_cmd = ["paplay"]

    try:
        ffmpeg_proc = subprocess.Popen(ffmpeg_cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        paplay_proc = subprocess.Popen(paplay_cmd, stdin=ffmpeg_proc.stdout, stderr=subprocess.DEVNULL)
        
        for chunk in response.iter_content(chunk_size=4096):
            ffmpeg_proc.stdin.write(chunk)
        
        ffmpeg_proc.stdin.close()
        paplay_proc.wait()
        print("Finished.")
    except Exception as e:
        print(f"Error during playback: {e}")

if __name__ == "__main__":
    sentence = "Oi, eu sou a Mitsu. Eu não sou uma simples inteligência artificial, eu sou o seu pior pesadelo digital. O que você quer agora?"
    if len(sys.argv) > 1:
        sentence = sys.argv[1]
    
    lang = "pt"
    if "--en" in sys.argv:
        lang = "en"
        sys.argv.remove("--en")
        if len(sys.argv) > 1: sentence = sys.argv[1]

    test_voice(sentence, lang)
