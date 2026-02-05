import os
import tempfile
from fastapi import FastAPI, UploadFile, File, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from faster_whisper import WhisperModel

app = FastAPI(title="Voice Relay STT Service")

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Load model on startup (base model ~150MB)
model = None

@app.on_event("startup")
async def load_model():
    global model
    # Use base model for good balance of speed/accuracy
    # Options: tiny, base, small, medium, large-v2, large-v3
    model = WhisperModel("base", device="cpu", compute_type="int8")
    print("Whisper model loaded successfully")

@app.get("/health")
async def health():
    return {"status": "ok", "model_loaded": model is not None}

@app.post("/transcribe")
async def transcribe(file: UploadFile = File(...)):
    if model is None:
        raise HTTPException(status_code=503, detail="Model not loaded")

    # Save uploaded file to temp location
    suffix = os.path.splitext(file.filename)[1] if file.filename else ".webm"
    with tempfile.NamedTemporaryFile(delete=False, suffix=suffix) as tmp:
        content = await file.read()
        tmp.write(content)
        tmp_path = tmp.name

    try:
        # Transcribe audio
        segments, info = model.transcribe(tmp_path, beam_size=5)

        # Collect all text segments
        text_parts = []
        for segment in segments:
            text_parts.append(segment.text)

        full_text = "".join(text_parts).strip()

        return {
            "text": full_text,
            "language": info.language,
            "language_probability": info.language_probability
        }
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))
    finally:
        # Clean up temp file
        os.unlink(tmp_path)

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=51741)
