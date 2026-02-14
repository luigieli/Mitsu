from faster_whisper import WhisperModel
from flask import Flask, request, jsonify
import io
import time
import os

app = Flask(__name__)

# --- CONFIGURATION ---
# Ryzen 5 5600 has 6 cores. We use 4 threads per model to leave 
# 2 cores free for the OS/Go/Brain. This prevents stuttering.
THREADS = 4 
DEVICE = "cpu"
COMPUTE = "int8"

print(f"🚀 [INIT] Loading Models on {DEVICE} with {THREADS} threads...")

# 1. LOAD ENGLISH SPECIALIST (The Speed Demon)
# distil-small.en is ~60% faster than standard small.
print("   - Loading 'distil-small.en'...")
model_en = WhisperModel("distil-small.en", device=DEVICE, compute_type=COMPUTE, cpu_threads=THREADS)

# 2. LOAD PORTUGUESE GENERALIST (The Polyglot)
# Standard 'small' is best for PT. 
print("   - Loading 'small' (Portuguese)...")
model_pt = WhisperModel("small", device=DEVICE, compute_type=COMPUTE, cpu_threads=THREADS)

# Global State
current_mode = "en" # Start in English
print("✅ [READY] Aura Ears Online. Default: English")

@app.route('/swap/<lang>', methods=['POST'])
def swap_language(lang):
    global current_mode
    if lang in ["en", "pt"]:
        current_mode = lang
        print(f"🔄 [HOTSWAP] Language set to: {lang.upper()}")
        return jsonify({"status": "ok", "mode": lang})
    return jsonify({"error": "Invalid language. Use 'en' or 'pt'"}), 400

@app.route('/transcribe', methods=['POST'])
def transcribe():
    if 'audio' not in request.files:
        return jsonify({"text": ""}), 400
    
    audio_file = request.files['audio']
    audio_bytes = audio_file.read()
    audio_stream = io.BytesIO(audio_bytes)
    
    start = time.time()
    
    # 2. HOTSWAP LOGIC
    # No loading time. Just pointer selection.
    if current_mode == "en":
        # English: Beam 1 is enough for Distil model
        segments, info = model_en.transcribe(
            audio_stream, 
            beam_size=1, 
            language="en", 
            condition_on_previous_text=False,
            vad_filter=False # You have Go VAD
        )
    else:
        # Portuguese: We need standard small
        segments, info = model_pt.transcribe(
            audio_stream, 
            beam_size=1, 
            language="pt",
            condition_on_previous_text=False,
            vad_filter=False
        )

    # 3. Format Output
    text = "".join([s.text for s in segments]).strip()
    duration = time.time() - start
    
    if text:
        print(f"🎤 [{current_mode.upper()}] Time: {duration:.3f}s | Text: {text}")
    
    return jsonify({
        "text": text, 
        "language": current_mode, 
        "duration": duration
    })

if __name__ == "__main__":
    # Threaded=True is important for Flask to handle requests while processing
    app.run(host='0.0.0.0', port=5001, threaded=True)
