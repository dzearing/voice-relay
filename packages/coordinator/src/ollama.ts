const OLLAMA_URL = process.env.OLLAMA_URL || "http://localhost:11434";
const OLLAMA_MODEL = process.env.OLLAMA_MODEL || "qwen3:0.6b";

const CLEANUP_PROMPT = `You clean up transcribed speech. Rules:
1. Remove filler words (uh, um, like, you know)
2. When speaker says "I mean X" or "no wait X", replace the previous word with X
3. Keep the sentence structure, just fix the corrected words

Examples:
Input: "I want to uh go to the store"
Output: I want to go to the store

Input: "I like red, I mean blue"
Output: I like blue.

Input: "Send it to John, no wait, send it to Mary"
Output: Send it to Mary.

Input: "The meeting is at um 3 PM"
Output: The meeting is at 3 PM.

Input: "I like red, I mean green"
Output: I like green.

Now clean this (output ONLY the cleaned sentence, nothing else):
Input: "`;

export async function cleanupText(rawText: string): Promise<string> {
  try {
    const response = await fetch(`${OLLAMA_URL}/api/generate`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model: OLLAMA_MODEL,
        prompt: CLEANUP_PROMPT + rawText + '"\nOutput:',
        stream: false,
      }),
    });

    if (!response.ok) {
      console.error("Ollama error, returning raw text");
      return rawText;
    }

    const data = await response.json();
    let result = data.response?.trim() || rawText;

    // Handle models that repeat the output format
    if (result.includes("Output:")) {
      const parts = result.split("Output:");
      result = parts[parts.length - 1].trim();
    }

    // Remove any leading/trailing quotes
    result = result.replace(/^["']|["']$/g, "");

    console.log(`Ollama cleanup: "${rawText}" → "${result}"`);
    return result || rawText;
  } catch (error) {
    console.error("Ollama unavailable, returning raw text:", error);
    return rawText;
  }
}
