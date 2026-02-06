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
        + app icon PNGs for macOS .icns (16,32,128,256,512,1024)
"""

from PIL import Image, ImageDraw
import math
import os

VARIANTS = {
    "connected": (0, 212, 255, 255),     # cyan #00d4ff
    "disconnected": (128, 128, 128, 255), # gray #808080
}
TRAY_SIZES = [16, 22, 32]
ICNS_SIZES = [16, 32, 128, 256, 512, 1024]

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


def draw_app_icon(size: int) -> Image.Image:
    """Draw the app icon at the given size — mic on a rounded dark background."""
    scale = 4
    ss = size * scale
    img = Image.new("RGBA", (ss, ss), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    # Rounded rectangle background
    bg_color = (30, 30, 40, 255)        # dark blue-gray
    corner_radius = ss * 0.22           # macOS-style rounding
    draw.rounded_rectangle(
        [0, 0, ss - 1, ss - 1],
        radius=round(corner_radius),
        fill=bg_color,
    )

    # Draw mic icon centered with padding
    padding = ss * 0.18
    icon_area = ss - 2 * padding
    s = icon_area / 24.0
    ox = padding  # offset x
    oy = padding  # offset y

    color = (0, 212, 255, 255)  # cyan
    sw = 1.5 * s
    lw = max(1, round(sw))

    # Mic body
    draw.rounded_rectangle(
        [ox + 9 * s, oy + 2 * s, ox + 15 * s, oy + 12 * s],
        radius=round(3 * s),
        outline=color,
        width=lw,
    )

    # Pickup arc vertical lines
    draw.line([(ox + 5 * s, oy + 10 * s), (ox + 5 * s, oy + 12 * s)], fill=color, width=lw)
    draw.line([(ox + 19 * s, oy + 10 * s), (ox + 19 * s, oy + 12 * s)], fill=color, width=lw)

    # Pickup arc
    draw.arc(
        [ox + 5 * s, oy + 5 * s, ox + 19 * s, oy + 19 * s],
        start=0, end=180, fill=color, width=lw,
    )

    # Stem
    draw.line([(ox + 12 * s, oy + 19 * s), (ox + 12 * s, oy + 22 * s)], fill=color, width=lw)

    img = img.resize((size, size), Image.LANCZOS)
    return img


def main():
    script_dir = os.path.dirname(os.path.abspath(__file__))

    # Systray icons
    for variant_name, color in VARIANTS.items():
        for size in TRAY_SIZES:
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

    # macOS app icon PNGs (for iconutil to pack into .icns)
    iconset_dir = os.path.join(script_dir, "AppIcon.iconset")
    os.makedirs(iconset_dir, exist_ok=True)

    # iconutil expects specific filenames: icon_NxN.png and icon_NxN@2x.png
    iconset_map = {
        16:   "icon_16x16.png",
        32:   "icon_16x16@2x.png",
        32:   "icon_32x32.png",
        64:   "icon_32x32@2x.png",
        128:  "icon_128x128.png",
        256:  "icon_128x128@2x.png",
        256:  "icon_256x256.png",
        512:  "icon_256x256@2x.png",
        512:  "icon_512x512.png",
        1024: "icon_512x512@2x.png",
    }
    # Use a list of tuples to avoid dict key deduplication
    iconset_entries = [
        (16,   "icon_16x16.png"),
        (32,   "icon_16x16@2x.png"),
        (32,   "icon_32x32.png"),
        (64,   "icon_32x32@2x.png"),
        (128,  "icon_128x128.png"),
        (256,  "icon_128x128@2x.png"),
        (256,  "icon_256x256.png"),
        (512,  "icon_256x256@2x.png"),
        (512,  "icon_512x512.png"),
        (1024, "icon_512x512@2x.png"),
    ]
    for px_size, filename in iconset_entries:
        img = draw_app_icon(px_size)
        filepath = os.path.join(iconset_dir, filename)
        img.save(filepath, "PNG")
        print(f"Generated AppIcon.iconset/{filename} ({px_size}x{px_size})")

    print("Done! Run 'iconutil -c icns AppIcon.iconset' on macOS to create AppIcon.icns")


if __name__ == "__main__":
    main()
