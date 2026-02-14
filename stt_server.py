from faster_whisper import WhisperModel
from flask import Flask, request, jsonify
import io
import time
import os

# Load model on CPU with INT8 quantization
model_size = "small"
threads = 4
print(f"Loading Faster-Whisper model: {model_size} (CPU/INT8) with {threads} threads...")
model = WhisperModel(
    model_size, 
    device="cpu", 
    compute_type="int8",
    cpu_threads=threads,
    num_workers=1
)

app = Flask(__name__)

@app.route('/transcribe', methods=['POST'])
def transcribe():
    if 'audio' not in request.files:
        return jsonify({"error": "No audio file"}), 400
        
    audio_file = request.files['audio']
    audio_bytes = audio_file.read()
    audio_stream = io.BytesIO(audio_bytes)
    
    start = time.time()
    # beam_size=1 for maximum speed, dynamic language detection, disabled internal VAD
    segments, info = model.transcribe(
        audio_stream, 
        beam_size=1, 
        best_of=1, 
        temperature=0, 
        condition_on_previous_text=False, 
        vad_filter=False
    )
    text = "".join([s.text for s in segments]).strip()
    
    duration = time.time() - start
    if text:
        print(f"STT ({info.language}): {text} [{duration:.2f}s]")
    
    return jsonify({
        "text": text,
        "language": info.language,
        "duration": duration
    })

if __name__ == "__main__":
    app.run(host='0.0.0.0', port=5001)
