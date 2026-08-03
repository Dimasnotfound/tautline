from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "assets" / "tautline.ico"


def draw_icon(size: int) -> Image.Image:
    scale = 4
    canvas_size = size * scale
    image = Image.new("RGBA", (canvas_size, canvas_size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    def p(value: float) -> int:
        return round(value / 32 * canvas_size)

    stroke = max(1, p(1.0))
    line_width = max(1, p(1.8))
    radius = p(7)
    black = (23, 23, 23, 255)
    white = (255, 255, 255, 255)

    draw.rounded_rectangle(
        (p(2.5), p(2.5), p(29.5), p(29.5)),
        radius=radius,
        fill=white,
        outline=black,
        width=stroke,
    )
    draw.line((p(8.5), p(9.5), p(8.5), p(22.5)), fill=black, width=line_width)
    draw.line((p(23.5), p(9.5), p(23.5), p(22.5)), fill=black, width=line_width)
    draw.line((p(8.5), p(16), p(23.5), p(16)), fill=black, width=line_width)
    draw.ellipse((p(13.4), p(13.4), p(18.6), p(18.6)), fill=black)

    return image.resize((size, size), Image.Resampling.LANCZOS)


def main() -> None:
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    sizes = [16, 24, 32, 48, 64, 128, 256]
    frames = [draw_icon(size) for size in sizes]
    frames[-1].save(OUTPUT, format="ICO", append_images=frames[:-1], sizes=[(s, s) for s in sizes])
    print(f"Generated {OUTPUT}")


if __name__ == "__main__":
    main()
