from faster_whisper import WhisperModel
from fastapi import FastAPI, HTTPException, Request
import io
import time
import asyncio
from concurrent.futures import ThreadPoolExecutor

app = FastAPI(title="Mitsu STT Server")

# --- CONFIGURATION ---
THREADS = 6 
DEVICE = "cpu"
COMPUTE = "int8"

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
    model = MODELS.get(mode)
    if not model:
        return ""

    audio_stream = io.BytesIO(audio_data)
    segments, _ = model.transcribe(
        audio_stream, 
        beam_size=1, 
        language=mode, 
        condition_on_previous_text=False,
        initial_prompt=initial_prompt,
        vad_filter=True,
        vad_parameters=dict(min_silence_duration_ms=500),
        no_speech_threshold=0.6
    )
    return "".join([s.text for s in segments]).strip()

@app.post("/transcribe")
async def transcribe(request: Request):
    # Fail Fast: Ensure we have audio data
    audio_bytes = await request.body()
    if not audio_bytes:
        return {"text": "", "error": "No audio data received"}

    initial_prompt = request.headers.get("X-Initial-Prompt")
    start = time.time()
    
    loop = asyncio.get_running_loop()
    text = await loop.run_in_executor(executor, run_transcription, audio_bytes, current_mode, initial_prompt)
    
    duration = time.time() - start
    if text:
        print(f"🎤 [{current_mode.upper()}] Time: {duration:.3f}s | Text: {text}")
    
    return {"text": text, "language": current_mode, "duration": duration}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=5001)
