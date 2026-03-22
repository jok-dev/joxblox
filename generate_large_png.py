#!/usr/bin/env python3
"""
Generate a large file-size PNG at 1024x1024.

This script uses random RGBA pixels (incompressible data) and saves with
compress_level=0 to maximize output size for a PNG.
"""

from __future__ import annotations

import argparse
import os
import random
import sys


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate a large 1024x1024 PNG.")
    parser.add_argument(
        "--width",
        type=int,
        default=1024,
        help="Image width in pixels (default: 1024)",
    )
    parser.add_argument(
        "--height",
        type=int,
        default=1024,
        help="Image height in pixels (default: 1024)",
    )
    parser.add_argument(
        "--output",
        default="large_1024.png",
        help="Output PNG file path (default: large_1024.png)",
    )
    parser.add_argument(
        "--seed",
        type=int,
        default=None,
        help="Optional seed for reproducible output",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    if args.seed is not None:
        random.seed(args.seed)

    try:
        from PIL import Image
    except ImportError:
        print("Pillow is required. Install with: pip install pillow", file=sys.stderr)
        return 1

    width = args.width
    height = args.height
    if width <= 0 or height <= 0:
        print("Width and height must be positive integers.", file=sys.stderr)
        return 1
    channel_count = 4  # RGBA8
    pixel_count = width * height

    # Random bytes are hard to compress, producing a larger PNG.
    pixel_bytes = os.urandom(pixel_count * channel_count)
    image = Image.frombytes("RGBA", (width, height), pixel_bytes)

    image.save(args.output, format="PNG", optimize=False, compress_level=0)

    output_size = os.path.getsize(args.output)
    print(f"Wrote {args.output}")
    print(f"Dimensions: {width}x{height}")
    print(f"File size: {output_size:,} bytes ({output_size / (1024 * 1024):.2f} MiB)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
