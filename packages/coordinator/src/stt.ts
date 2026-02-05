const STT_URL = process.env.STT_URL || "http://localhost:51741";

export interface TranscriptionResult {
  text: string;
  language: string;
  language_probability: number;
}

export async function transcribe(
  audioBuffer: Buffer,
  filename: string
): Promise<TranscriptionResult> {
  const formData = new FormData();
  const blob = new Blob([new Uint8Array(audioBuffer)]);
  formData.append("file", blob, filename);

  const response = await fetch(`${STT_URL}/transcribe`, {
    method: "POST",
    body: formData,
  });

  if (!response.ok) {
    const error = await response.text();
    throw new Error(`STT service error: ${error}`);
  }

  return response.json();
}
