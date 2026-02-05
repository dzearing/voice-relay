import { Hardware } from "keysender";
import clipboard from "clipboardy";

const hardware = new Hardware();

export async function typeText(text: string, delay: number = 20): Promise<void> {
  // keysender's printText types character by character
  hardware.keyboard.printText(text, delay);
}

export async function pasteFromClipboard(): Promise<void> {
  // Simulate Ctrl+V to paste
  await hardware.keyboard.sendKey(["ctrl", "v"]);
}

export async function copyToClipboard(text: string): Promise<void> {
  await clipboard.write(text);
}

export type OutputMode = "type" | "paste";

export async function outputText(
  text: string,
  options: { mode: OutputMode; copy: boolean; delay: number }
): Promise<void> {
  const { mode, copy, delay } = options;

  // Always copy to clipboard first (needed for paste mode, optional for type mode)
  if (copy || mode === "paste") {
    await copyToClipboard(text);
    console.log("Text copied to clipboard");
  }

  if (mode === "paste") {
    console.log("Pasting text...");
    await pasteFromClipboard();
    console.log("Done pasting");
  } else if (mode === "type") {
    console.log("Typing text...");
    await typeText(text, delay);
    console.log("Done typing");
  }
}
