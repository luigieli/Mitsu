import asyncio
import json
import numpy as np
import sherpa_onnx
import websockets
import os

# --- CONFIGURATION ---
MODEL_DIR = os.getenv("MODEL_DIR", "/app/models/sherpa-onnx-streaming-zipformer-en-2023-06-26")

def create_recognizer():
    return sherpa_onnx.OnlineRecognizer.from_transducer(
        tokens=f"{MODEL_DIR}/tokens.txt",
        encoder=f"{MODEL_DIR}/encoder-epoch-99-avg-1-chunk-16-left-128.onnx",
        decoder=f"{MODEL_DIR}/decoder-epoch-99-avg-1-chunk-16-left-128.onnx",
        joiner=f"{MODEL_DIR}/joiner-epoch-99-avg-1-chunk-16-left-128.onnx",
        num_threads=4,
        sample_rate=16000,
        feature_dim=80,
        enable_endpoint_detection=True,
        rule1_min_trailing_silence=2.4,
        rule2_min_trailing_silence=1.2,
        rule3_min_utterance_length=20.0,
        debug=False,
    )

recognizer = create_recognizer()

async def handle_connection(websocket):
    print("✅ [Sherpa] Client connected")
    stream = recognizer.create_stream()
    state = {"last_text": "", "frames": 0}
    
    try:
        async for message in websocket:
            if message == "FLUSH":
                stream = await flush_stream(websocket, stream, state)
                continue

            if isinstance(message, bytes):
                await process_audio_frame(websocket, stream, message, state)

    except websockets.exceptions.ConnectionClosed:
        print("❌ [Sherpa] Client disconnected")
    except Exception as e:
        print(f"⚠️ [Sherpa] Error: {e}")

async def process_audio_frame(websocket, stream, data, state):
    state["frames"] += 1
    samples = np.frombuffer(data, dtype=np.int16).astype(np.float32) / 32768.0
    stream.accept_waveform(16000, samples)
    
    while recognizer.is_ready(stream):
        recognizer.decode_stream(stream)
    
    text = recognizer.get_result(stream).strip()
    if text and text != state["last_text"]:
        await websocket.send(json.dumps({"text": text, "is_final": False}))
        state["last_text"] = text

async def flush_stream(websocket, stream, state):
    final_text = recognizer.get_result(stream).strip()
    print(f"DEBUG: Flush triggered. Final: {final_text}")
    await websocket.send(json.dumps({"text": final_text, "is_final": True}))
    
    # Reset for next utterance
    state["last_text"] = ""
    state["frames"] = 0
    # Note: sherpa-onnx streams usually need to be recreated or handled carefully
    # Re-creating the stream object is the safest 'reset'
    return recognizer.create_stream() 

async def main():
    async with websockets.serve(handle_connection, "0.0.0.0", 5002):
        print("🚀 [Sherpa] Native WebSocket Server running on port 5002")
        await asyncio.Future()

if __name__ == "__main__":
    asyncio.run(main())
