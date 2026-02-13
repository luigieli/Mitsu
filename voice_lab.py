import os
import json
import requests
import subprocess
import time
from flask import Flask, request, render_template_string, jsonify

app = Flask(__name__)

# Global process tracking
current_playback = []

DEFAULT_CONFIG = {
    "highpass": 150,
    "lowpass": 15000,
    "boxy_gain": -15,
    "presence_gain": 8,
    "sparkle_gain": 0,
    "air_gain": 0,
    "exciter_amount": 3.0,
    "deesser_intensity": 0.5,
    "gate_threshold": -80,
    "stereo_width": 2.0,
    "haas_delay": 10,
    "pitch": 1.25,
    "speed": 1.0,
    "formant_preserved": True,
    "pitch_quality": "quality",
    "loudnorm_i": -16,
    "echo_feedback": 0.01,
    "voice_model": "mitsu_custom",
    "lang_code": "p",
    "bypass": False
}

CONFIG_FILE = "voice_config.json"

def load_config():
    if os.path.exists(CONFIG_FILE):
        try:
            with open(CONFIG_FILE, "r") as f:
                return {**DEFAULT_CONFIG, **json.load(f)}
        except: pass
    return DEFAULT_CONFIG

@app.route("/")
def index():
    config = load_config()
    return render_template_string(HTML_TEMPLATE, config=config)

@app.route("/play", methods=["POST"])
def play():
    global current_playback
    for p in current_playback:
        try: p.kill()
        except: pass
    current_playback = []
    subprocess.run(["pkill", "ffplay"], stderr=subprocess.DEVNULL)

    data = request.json
    f_list = []
    
    if not data.get('bypass', False):
        f_list.append(f"highpass=f={data['highpass']}")
        f_list.append(f"lowpass=f={data['lowpass']}")
        f_list.append(f"equalizer=f=400:t=q:w=1:g={data['boxy_gain']}")
        f_list.append(f"equalizer=f=3000:t=q:w=1:g={data['presence_gain']}")
        f_list.append(f"equalizer=f=8000:t=q:w=1:g={data['sparkle_gain']}")
        
        formant_val = "preserved" if data.get('formant_preserved', True) else "shifted"
        f_list.append(f"rubberband=pitch={data['pitch']}:tempo={data['speed']}:formant={formant_val}:pitchq={data.get('pitch_quality', 'quality')}")
        
        if data['deesser_intensity'] > 0: f_list.append(f"deesser=i={data['deesser_intensity']}")
        if data['exciter_amount'] > 0: f_list.append(f"aexciter=amount={data['exciter_amount']}")
        
        f_list.append("pan=stereo|c0=c0|c1=c0")
        if data['haas_delay'] > 0: f_list.append(f"adelay=0|{data['haas_delay']}")
        if data['stereo_width'] > 1.0: f_list.append(f"extrastereo=m={data['stereo_width']}")
        
        f_list.append(f"agate=threshold={data['gate_threshold']}dB")
        f_list.append(f"loudnorm=I={data['loudnorm_i']}:LRA=11:TP=-1.5")
        if data['echo_feedback'] > 0.01:
            f_list.append(f"aecho=0.8:0.3:15:{data['echo_feedback']}")
    else:
        f_list.append("anull")

    filters = ",".join(f_list)

    try:
        payload = {
            "input": data.get("text", ""),
            "voice": data.get("voice_model", "mitsu_custom"),
            "lang_code": data.get("lang_code", "p"),
            "speed": 1.0, 
            "model": "kokoro"
        }
        response = requests.post("http://localhost:8880/v1/audio/speech", json=payload, stream=True)
        response.raise_for_status()
        ffmpeg_proc = subprocess.Popen(["ffmpeg", "-y", "-i", "pipe:0", "-af", filters, "-f", "wav", "pipe:1"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        ffplay_proc = subprocess.Popen(["ffplay", "-nodisp", "-autoexit", "-i", "pipe:0", "-v", "error"], stdin=ffmpeg_proc.stdout, stderr=subprocess.PIPE)
        current_playback = [ffmpeg_proc, ffplay_proc]
        for chunk in response.iter_content(chunk_size=8192):
            if ffmpeg_proc.poll() is not None: break
            ffmpeg_proc.stdin.write(chunk)
        ffmpeg_proc.stdin.close()
        return jsonify({"status": "success", "filters": filters})
    except Exception as e:
        return jsonify({"status": "error", "message": str(e)}), 500

@app.route("/save", methods=["POST"])
def save():
    with open(CONFIG_FILE, "w") as f: json.dump(request.json, f, indent=4)
    return jsonify({"status": "success"})

HTML_TEMPLATE = """
<!DOCTYPE html>
<html>
<head>
    <title>Mitsu Voice Tuner 80/10/10</title>
    <style>
        body { background: #000; color: #fff; font-family: 'Segoe UI', sans-serif; padding: 20px; overflow: hidden; }
        .grid { display: grid; grid-template-columns: 1fr 350px; gap: 20px; max-width: 1400px; margin: auto; height: calc(100vh - 40px); }
        .panel { background: #0a0a0a; padding: 20px; border-radius: 12px; border: 1px solid #00ff00; overflow-y: auto; scrollbar-width: thin; scrollbar-color: #00ff00 #000; }
        .row { display: flex; align-items: center; gap: 15px; margin-bottom: 8px; background: #111; padding: 8px; border-radius: 6px; }
        label { width: 200px; font-size: 0.7em; color: #00ff00; text-transform: uppercase; font-weight: bold; }
        input[type=range] { flex: 1; accent-color: #00ff00; }
        select, textarea { background: #000; color: #00ff00; border: 1px solid #00ff00; padding: 10px; border-radius: 4px; font-family: inherit; }
        .value { width: 60px; font-family: monospace; color: #00ff00; text-align: right; }
        textarea { width: 100%; margin-bottom: 10px; font-size: 1em; box-sizing: border-box; }
        button { padding: 15px; background: #00ff00; color: #000; border: none; cursor: pointer; font-weight: bold; width: 100%; border-radius: 6px; text-transform: uppercase; }
        .btn-reset { background: #ff4444; color: #fff; margin-top: 10px; }
        .section { color: #fff; font-size: 0.65em; margin-top: 15px; border-left: 3px solid #00ff00; padding-left: 10px; text-transform: uppercase; margin-bottom: 5px; }
        .checkbox-row { background: #1a1a1a; padding: 10px; border-radius: 6px; margin-bottom: 8px; display: flex; align-items: center; justify-content: space-between; font-size: 0.8em; }
    </style>
</head>
<body>
    <div class="grid">
        <div class="panel">
            <h1>💎 VOICE LAB: BALANCED ANIME</h1>
            <textarea id="text">Olá, humano. Estou usando 80% de Nezumi agora. Use o botão de reset para ouvir o áudio sem filtros.</textarea>
            
            <div class="section">Engine Configuration</div>
            <div class="row">
                <label>Voice Model</label>
                <select id="voice_model" style="flex: 1;">
                    <option value="mitsu_custom">Mitsu (80/10/10 Blend)</option>
                    <option value="af_heart">Amy (US)</option>
                    <option value="pf_dora">Dora (BR)</option>
                    <option value="jf_nezumi">Nezumi (JP)</option>
                </select>
            </div>
            <div class="row">
                <label>Language Phonemes</label>
                <select id="lang_code" style="flex: 1;">
                    <option value="p">Portuguese (BR)</option>
                    <option value="a">English (US)</option>
                </select>
            </div>

            <div class="section">Rubberband Engine</div>
            <div class="row"><label>Pitch</label><input type="range" id="pitch" min="0.5" max="3.0" step="0.01" value="{{config.pitch}}" oninput="uv(this)"><div class="value">{{config.pitch}}</div></div>
            <div class="row"><label>Speed</label><input type="range" id="speed" min="0.5" max="3.0" step="0.1" value="{{config.speed}}" oninput="uv(this)"><div class="value">{{config.speed}}</div></div>
            <div class="checkbox-row"><label>Preserve Formants</label><input type="checkbox" id="formant_preserved" checked></div>

            <div class="section">Equalizer</div>
            <div class="row"><label>Highpass</label><input type="range" id="highpass" min="20" max="1000" step="10" value="{{config.highpass}}" oninput="uv(this)"><div class="value">{{config.highpass}}</div></div>
            <div class="row"><label>Lowpass</label><input type="range" id="lowpass" min="5000" max="22000" step="500" value="{{config.lowpass}}" oninput="uv(this)"><div class="value">{{config.lowpass}}</div></div>
            <div class="row"><label>Boxy Cut</label><input type="range" id="boxy_gain" min="-60" max="20" step="1" value="{{config.boxy_gain}}" oninput="uv(this)"><div class="value">{{config.boxy_gain}}</div></div>
            <div class="row"><label>Presence (3kHz)</label><input type="range" id="presence_gain" min="-40" max="40" step="1" value="{{config.presence_gain}}" oninput="uv(this)"><div class="value">{{config.presence_gain}}</div></div>
            <div class="row"><label>Sparkle (8kHz)</label><input type="range" id="sparkle_gain" min="-40" max="40" step="1" value="{{config.sparkle_gain}}" oninput="uv(this)"><div class="value">{{config.sparkle_gain}}</div></div>

            <div class="section">Professional FX</div>
            <div class="row"><label>Exciter</label><input type="range" id="exciter_amount" min="0" max="10" step="0.1" value="{{config.exciter_amount}}" oninput="uv(this)"><div class="value">{{config.exciter_amount}}</div></div>
            <div class="row"><label>De-Esser</label><input type="range" id="deesser_intensity" min="0" max="5" step="0.1" value="{{config.deesser_intensity}}" oninput="uv(this)"><div class="value">{{config.deesser_intensity}}</div></div>
            <div class="row"><label>Stereo Width</label><input type="range" id="stereo_width" min="1.0" max="10.0" step="0.1" value="{{config.stereo_width}}" oninput="uv(this)"><div class="value">{{config.stereo_width}}</div></div>
            <div class="row"><label>Loudness (LUFS)</label><input type="range" id="loudnorm_i" min="-70" max="-5" step="1" value="{{config.loudnorm_i}}" oninput="uv(this)"><div class="value">{{config.loudnorm_i}}</div></div>

            <button onclick="play()" id="playBtn">⚡ PREVIEW MASTERPIECE</button>
            <button class="btn-reset" onclick="resetToRaw()">🔄 RESET TO RAW (NEUTRAL)</button>
            <button onclick="save()" style="margin-top:10px; background:#2196F3; color:white;">💾 SAVE TO MITSU</button>
        </div>

        <div class="panel" style="border-color: #333; font-size: 0.85em;">
            <h2 style="color: #00ff00; margin-top:0;">📖 INFO</h2>
            <p><b>Reset to Raw:</b> Instantly sets all filters to zero/neutral. Great for comparing your tune against the original voice.</p>
            <p><b>Current Blend:</b> 80% Nezumi (JP), 10% Dora (BR), 10% Bella (US).</p>
            <input type="checkbox" id="bypass" style="display:none">
            <input type="hidden" id="haas_delay" value="10">
            <input type="hidden" id="gate_threshold" value="-80">
            <input type="hidden" id="compand_p1" value="-20">
            <input type="hidden" id="compand_p2" value="-10">
            <input type="hidden" id="echo_feedback" value="0.01">
            <input type="hidden" id="pitch_quality" value="quality">
        </div>
    </div>
    <script>
        function uv(e){ e.parentElement.querySelector('.value').textContent = e.value; }
        
        function resetToRaw() {
            document.getElementById('pitch').value = 1.0;
            document.getElementById('speed').value = 1.0;
            document.getElementById('formant_preserved').checked = false;
            document.getElementById('highpass').value = 20;
            document.getElementById('lowpass').value = 22000;
            document.getElementById('boxy_gain').value = 0;
            document.getElementById('presence_gain').value = 0;
            document.getElementById('sparkle_gain').value = 0;
            document.getElementById('exciter_amount').value = 0;
            document.getElementById('deesser_intensity').value = 0;
            document.getElementById('stereo_width').value = 1.0;
            document.getElementById('loudnorm_i').value = -24;
            
            // Update all visible labels
            document.querySelectorAll('input[type=range]').forEach(uv);
            alert("Sliders reset to Neutral!");
        }

        async function play() {
            const btn = document.getElementById('playBtn');
            btn.textContent = "⌛ MASTERING...";
            const c = { 
                text: document.getElementById('text').value, 
                voice_model: document.getElementById('voice_model').value,
                lang_code: document.getElementById('lang_code').value,
                bypass: document.getElementById('bypass').checked,
                pitch: parseFloat(document.getElementById('pitch').value), 
                speed: parseFloat(document.getElementById('speed').value), 
                formant_preserved: document.getElementById('formant_preserved').checked,
                highpass: parseInt(document.getElementById('highpass').value), 
                lowpass: parseInt(document.getElementById('lowpass').value), 
                boxy_gain: parseInt(document.getElementById('boxy_gain').value), 
                presence_gain: parseInt(document.getElementById('presence_gain').value), 
                sparkle_gain: parseInt(document.getElementById('sparkle_gain').value), 
                exciter_amount: parseFloat(document.getElementById('exciter_amount').value), 
                deesser_intensity: parseFloat(document.getElementById('deesser_intensity').value), 
                stereo_width: parseFloat(document.getElementById('stereo_width').value), 
                loudnorm_i: parseInt(document.getElementById('loudnorm_i').value),
                gate_threshold: -80, haas_delay: 10, vibrato_depth: 0, echo_feedback: 0.01, pitch_quality: "quality"
            };
            await fetch('/play', { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(c) });
            btn.textContent = "⚡ PREVIEW MASTERPIECE";
        }
        async function save() {
            /* same save logic as play */
            const c = { voice_model: document.getElementById('voice_model').value, lang_code: document.getElementById('lang_code').value, pitch: parseFloat(document.getElementById('pitch').value), speed: parseFloat(document.getElementById('speed').value), formant_preserved: document.getElementById('formant_preserved').checked, highpass: parseInt(document.getElementById('highpass').value), lowpass: parseInt(document.getElementById('lowpass').value), boxy_gain: parseInt(document.getElementById('boxy_gain').value), presence_gain: parseInt(document.getElementById('presence_gain').value), sparkle_gain: parseInt(document.getElementById('sparkle_gain').value), exciter_amount: parseFloat(document.getElementById('exciter_amount').value), deesser_intensity: parseFloat(document.getElementById('deesser_intensity').value), stereo_width: parseFloat(document.getElementById('stereo_width').value), loudnorm_i: parseInt(document.getElementById('loudnorm_i').value) };
            await fetch('/save', { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(c) });
            alert("Saved!");
        }
    </script>
</body>
</html>
"""

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
