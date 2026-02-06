#!/usr/bin/env python3
"""
Render microphone SVG icon to PNG at multiple sizes and color variants.
Uses Pillow to draw the Lucide-style mic icon matching the PWA's SVG:

  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"
       stroke-linecap="round" stroke-linejoin="round">
    <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z"/>
    <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
    <line x1="12" x2="12" y1="19" y2="22"/>
  </svg>

Output: icon_{connected,disconnected}_{16,22,32}.png + .ico files for Windows
"""

from PIL import Image, ImageDraw
import math
import os

VARIANTS = {
    "connected": (0, 212, 255, 255),     # cyan #00d4ff
    "disconnected": (128, 128, 128, 255), # gray #808080
}
SIZES = [16, 22, 32]

# SVG viewBox is 0 0 24 24, stroke-width 1.5


def draw_mic_icon(size: int, color: tuple[int, int, int, int]) -> Image.Image:
    """Draw the Lucide mic icon at the given size with the given color."""
    # Use supersampling for anti-aliasing: draw at 4x then downsample
    scale = 4
    ss = size * scale
    img = Image.new("RGBA", (ss, ss), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    # Scale factor from SVG coords (24x24) to our supersample coords
    s = ss / 24.0
    sw = 1.5 * s  # stroke width scaled

    # --- Mic body: rounded rect (path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z")
    # This is a rectangle from x=9..15, y=2..12 with 3-unit radius rounded ends
    # Top semicircle center at (12, 5), bottom semicircle center at (12, 9), radius 3
    # Left side at x=9, right side at x=15

    # Draw as a rounded rectangle
    mic_left = 9 * s
    mic_right = 15 * s
    mic_top = 2 * s
    mic_bottom = 12 * s
    mic_radius = 3 * s

    draw.rounded_rectangle(
        [mic_left, mic_top, mic_right, mic_bottom],
        radius=mic_radius,
        outline=color,
        width=max(1, round(sw)),
    )

    # --- Pickup arc: path d="M19 10v2a7 7 0 0 1-14 0v-2"
    # Left vertical line from (5, 10) to (5, 12)
    # Right vertical line from (19, 10) to (19, 12)
    # Arc from (5, 12) to (19, 12) centered at (12, 12) radius 7, going below

    # Draw the two short vertical lines
    lw = max(1, round(sw))
    draw.line([(5 * s, 10 * s), (5 * s, 12 * s)], fill=color, width=lw)
    draw.line([(19 * s, 10 * s), (19 * s, 12 * s)], fill=color, width=lw)

    # Draw the arc (bottom half of ellipse from 5,12 to 19,12)
    # Arc bounding box: center (12,12), radius 7 -> bbox (5, 5) to (19, 19)
    # We want the bottom arc from angle 0 to 180 (in Pillow: 0=3 o'clock)
    arc_bbox = [5 * s, 5 * s, 19 * s, 19 * s]
    draw.arc(arc_bbox, start=0, end=180, fill=color, width=lw)

    # --- Stem: line x1="12" x2="12" y1="19" y2="22"
    draw.line([(12 * s, 19 * s), (12 * s, 22 * s)], fill=color, width=lw)

    # Downsample with LANCZOS for anti-aliasing
    img = img.resize((size, size), Image.LANCZOS)
    return img


def main():
    script_dir = os.path.dirname(os.path.abspath(__file__))

    for variant_name, color in VARIANTS.items():
        for size in SIZES:
            img = draw_mic_icon(size, color)
            filename = f"icon_{variant_name}_{size}.png"
            filepath = os.path.join(script_dir, filename)
            img.save(filepath, "PNG")
            print(f"Generated {filename} ({size}x{size})")

        # Generate .ico for Windows (contains 16, 32 sizes)
        ico_16 = draw_mic_icon(16, color)
        ico_32 = draw_mic_icon(32, color)
        ico_filename = f"icon_{variant_name}.ico"
        ico_path = os.path.join(script_dir, ico_filename)
        ico_32.save(ico_path, format="ICO", sizes=[(16, 16), (32, 32)],
                    append_images=[ico_16])
        print(f"Generated {ico_filename}")

    print("Done!")


if __name__ == "__main__":
    main()
