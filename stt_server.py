import asyncio
import numpy as np
import io
import time
from concurrent.futures import ThreadPoolExecutor
from faster_whisper import WhisperModel
from fastapi import FastAPI, HTTPException, Request

app = FastAPI(title="Mitsu STT Server")

# --- CONFIGURATION ---
THREADS = 6 
DEVICE = "cpu"
COMPUTE = "int8"
DEFAULT_INITIAL_PROMPT = "Pokémon, FireRed, Game Boy, Nintendo, Pikachu, Bulbasaur, Charmander, Squirtle."

# Model Registry to avoid branching logic
MODELS = {
    "en": WhisperModel("distil-small.en", device=DEVICE, compute_type=COMPUTE, cpu_threads=THREADS),
    "pt": WhisperModel("small", device=DEVICE, compute_type=COMPUTE, cpu_threads=THREADS)
}

executor = ThreadPoolExecutor(max_workers=2)
current_mode = "en"

@app.post("/swap/{lang}")
async def swap_language(lang: str):
    global current_mode
    if lang not in MODELS:
        raise HTTPException(status_code=400, detail="Invalid language. Use 'en' or 'pt'")
    
    current_mode = lang
    print(f"🔄 [HOTSWAP] Language set to: {lang.upper()}")
    return {"status": "ok", "mode": lang}

def run_transcription(audio_data, mode, initial_prompt=None):
    # Use the 'pt' model for transcription if we want to support both well, 
    # as 'small' is multilingual while 'distil-small.en' is English-only.
    # However, to be efficient, we can use a heuristic or just use the multilingual 'small' model for all detections.
    model = MODELS.get("pt") # 'small' is the multilingual model
    
    if initial_prompt is None:
        initial_prompt = DEFAULT_INITIAL_PROMPT

    # Convert raw s16le bytes to float32 numpy array
    audio_np = np.frombuffer(audio_data, dtype=np.int16).astype(np.float32) / 32768.0

    # Auto-detect language but limit to relevant ones if needed
    # Whisper does this naturally.
    segments, info = model.transcribe(
        audio_np, 
        beam_size=5, 
        language=None, # Auto-detect
        condition_on_previous_text=False,
        initial_prompt=initial_prompt,
        vad_filter=True,
        vad_parameters=dict(min_silence_duration_ms=500),
        no_speech_threshold=0.6
    )
    
    text = "".join([s.text for s in segments]).strip()
    detected_lang = info.language
    
    return text, detected_lang

@app.post("/transcribe")
async def transcribe(request: Request):
    # Fail Fast: Ensure we have audio data
    audio_bytes = await request.body()
    if not audio_bytes:
        return {"text": "", "error": "No audio data received"}

    initial_prompt = request.headers.get("X-Initial-Prompt")
    start = time.time()
    
    loop = asyncio.get_running_loop()
    text, detected_lang = await loop.run_in_executor(executor, run_transcription, audio_bytes, current_mode, initial_prompt)
    
    duration = time.time() - start
    if text:
        print(f"🎤 [{detected_lang.upper()} detected] Time: {duration:.3f}s | Text: {text}")
    
    return {"text": text, "language": detected_lang, "duration": duration}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=5001)
