"""Analyze a RenderDoc zip.xml capture: summarize Texture2D allocations by format and total VRAM bytes."""

import sys
import xml.etree.ElementTree as ET
from collections import defaultdict

# Bytes per pixel (or per-block accounting) for common DXGI formats.
# Block-compressed formats: 4x4 pixel blocks; BC1/BC4 = 8 bytes/block => 0.5 B/px;
# BC2/BC3/BC5/BC6H/BC7 = 16 bytes/block => 1 B/px.
BC_FORMATS = {
    "DXGI_FORMAT_BC1_TYPELESS": 0.5, "DXGI_FORMAT_BC1_UNORM": 0.5, "DXGI_FORMAT_BC1_UNORM_SRGB": 0.5,
    "DXGI_FORMAT_BC2_TYPELESS": 1.0, "DXGI_FORMAT_BC2_UNORM": 1.0, "DXGI_FORMAT_BC2_UNORM_SRGB": 1.0,
    "DXGI_FORMAT_BC3_TYPELESS": 1.0, "DXGI_FORMAT_BC3_UNORM": 1.0, "DXGI_FORMAT_BC3_UNORM_SRGB": 1.0,
    "DXGI_FORMAT_BC4_TYPELESS": 0.5, "DXGI_FORMAT_BC4_UNORM": 0.5, "DXGI_FORMAT_BC4_SNORM": 0.5,
    "DXGI_FORMAT_BC5_TYPELESS": 1.0, "DXGI_FORMAT_BC5_UNORM": 1.0, "DXGI_FORMAT_BC5_SNORM": 1.0,
    "DXGI_FORMAT_BC6H_TYPELESS": 1.0, "DXGI_FORMAT_BC6H_UF16": 1.0, "DXGI_FORMAT_BC6H_SF16": 1.0,
    "DXGI_FORMAT_BC7_TYPELESS": 1.0, "DXGI_FORMAT_BC7_UNORM": 1.0, "DXGI_FORMAT_BC7_UNORM_SRGB": 1.0,
}

UNCOMPRESSED_FORMATS = {
    "DXGI_FORMAT_R8_UNORM": 1, "DXGI_FORMAT_R8_UINT": 1, "DXGI_FORMAT_R8_SNORM": 1, "DXGI_FORMAT_R8_SINT": 1, "DXGI_FORMAT_A8_UNORM": 1,
    "DXGI_FORMAT_R8G8_UNORM": 2, "DXGI_FORMAT_R8G8_UINT": 2, "DXGI_FORMAT_R16_UNORM": 2, "DXGI_FORMAT_R16_FLOAT": 2, "DXGI_FORMAT_R16_UINT": 2, "DXGI_FORMAT_D16_UNORM": 2,
    "DXGI_FORMAT_R8G8B8A8_UNORM": 4, "DXGI_FORMAT_R8G8B8A8_UNORM_SRGB": 4, "DXGI_FORMAT_R8G8B8A8_TYPELESS": 4, "DXGI_FORMAT_R8G8B8A8_UINT": 4, "DXGI_FORMAT_R8G8B8A8_SNORM": 4,
    "DXGI_FORMAT_B8G8R8A8_UNORM": 4, "DXGI_FORMAT_B8G8R8A8_UNORM_SRGB": 4, "DXGI_FORMAT_B8G8R8A8_TYPELESS": 4, "DXGI_FORMAT_B8G8R8X8_UNORM": 4,
    "DXGI_FORMAT_R10G10B10A2_UNORM": 4, "DXGI_FORMAT_R10G10B10A2_TYPELESS": 4, "DXGI_FORMAT_R11G11B10_FLOAT": 4,
    "DXGI_FORMAT_R16G16_UNORM": 4, "DXGI_FORMAT_R16G16_FLOAT": 4, "DXGI_FORMAT_R32_FLOAT": 4, "DXGI_FORMAT_R32_UINT": 4, "DXGI_FORMAT_D32_FLOAT": 4, "DXGI_FORMAT_D24_UNORM_S8_UINT": 4, "DXGI_FORMAT_R24G8_TYPELESS": 4,
    "DXGI_FORMAT_R16G16B16A16_UNORM": 8, "DXGI_FORMAT_R16G16B16A16_FLOAT": 8, "DXGI_FORMAT_R16G16B16A16_TYPELESS": 8, "DXGI_FORMAT_R32G32_FLOAT": 8, "DXGI_FORMAT_R32G32_UINT": 8, "DXGI_FORMAT_D32_FLOAT_S8X24_UINT": 8, "DXGI_FORMAT_R32G8X24_TYPELESS": 8,
    "DXGI_FORMAT_R32G32B32A32_FLOAT": 16, "DXGI_FORMAT_R32G32B32A32_UINT": 16, "DXGI_FORMAT_R32G32B32A32_TYPELESS": 16,
}

def bytes_per_pixel(fmt: str):
    if fmt in BC_FORMATS:
        return BC_FORMATS[fmt]
    if fmt in UNCOMPRESSED_FORMATS:
        return UNCOMPRESSED_FORMATS[fmt]
    return None  # unknown; will be flagged

def is_bc(fmt: str) -> bool:
    return fmt in BC_FORMATS

def texture_bytes(width: int, height: int, mip_levels: int, array_size: int, fmt: str) -> int:
    bpp = bytes_per_pixel(fmt)
    if bpp is None:
        return 0
    if mip_levels <= 0:
        # 0 means "full chain"; we approximate with 1.33x base level.
        # Use computed full chain.
        mip_count = 1
        w, h = width, height
        while w > 1 or h > 1:
            w = max(1, w // 2)
            h = max(1, h // 2)
            mip_count += 1
        mip_levels = mip_count
    total = 0.0
    w, h = width, height
    for _ in range(mip_levels):
        if is_bc(fmt):
            bw = max(1, (w + 3) // 4)
            bh = max(1, (h + 3) // 4)
            # bpp for BC formats here is bytes per pixel; convert to bytes-per-block
            block_bytes = bpp * 16  # 4x4 block
            total += bw * bh * block_bytes
        else:
            total += w * h * bpp
        w = max(1, w // 2)
        h = max(1, h // 2)
    return int(total * array_size)

def parse(path: str):
    tree = ET.parse(path)
    root = tree.getroot()
    textures = []
    unknown_formats = defaultdict(int)
    for chunk in root.iter("chunk"):
        name = chunk.get("name", "")
        if name != "ID3D11Device::CreateTexture2D":
            continue
        desc = chunk.find("struct[@name='Descriptor']")
        if desc is None:
            continue
        def u(field):
            e = desc.find(f"uint[@name='{field}']")
            return int(e.text) if e is not None else 0
        def en(field):
            e = desc.find(f"enum[@name='{field}']")
            return e.get("string", "") if e is not None else ""
        def bf(field):
            e = desc.find(f"enum[@name='{field}']")
            return e.get("string", "") if e is not None else ""
        width = u("Width")
        height = u("Height")
        mip_levels = u("MipLevels")
        array_size = u("ArraySize")
        fmt = en("Format")
        usage = en("Usage")
        bind = bf("BindFlags")
        misc = bf("MiscFlags")
        rid = chunk.find("ResourceId[@name='pTexture']")
        resource_id = rid.text if rid is not None else "?"
        if bytes_per_pixel(fmt) is None:
            unknown_formats[fmt] += 1
        b = texture_bytes(width, height, mip_levels, array_size, fmt)
        textures.append({
            "id": resource_id, "width": width, "height": height, "mips": mip_levels,
            "array": array_size, "format": fmt, "usage": usage, "bind": bind,
            "misc": misc, "bytes": b,
        })
    return textures, unknown_formats

def categorize(t):
    bind = t["bind"]
    misc = t["misc"]
    fmt = t["format"]
    is_cube = "TEXTURECUBE" in misc
    is_rt = "RENDER_TARGET" in bind
    is_ds = "DEPTH_STENCIL" in bind
    is_sr = "SHADER_RESOURCE" in bind
    if is_cube:
        return "cubemap (skybox/probe)"
    if is_ds:
        return "depth/stencil target"
    if is_rt and not is_bc(fmt):
        return "render target (screen-space)"
    if is_bc(fmt) and is_sr:
        return "asset texture (BC-compressed)"
    if not is_bc(fmt) and is_sr and t["width"] >= 64 and t["height"] >= 64:
        return "asset texture (uncompressed)"
    return "small/utility"

def main():
    path = sys.argv[1]
    textures, unknown = parse(path)
    print(f"Total Texture2D creates: {len(textures)}")
    print(f"Total VRAM (all): {sum(t['bytes'] for t in textures)/1024/1024:.2f} MB")
    print()

    # By category
    by_cat = defaultdict(lambda: {"count": 0, "bytes": 0})
    for t in textures:
        c = categorize(t)
        by_cat[c]["count"] += 1
        by_cat[c]["bytes"] += t["bytes"]
    print("=== By category ===")
    for cat, info in sorted(by_cat.items(), key=lambda kv: -kv[1]["bytes"]):
        print(f"  {cat:40s} {info['count']:5d} textures  {info['bytes']/1024/1024:8.2f} MB")
    print()

    # By format
    by_fmt = defaultdict(lambda: {"count": 0, "bytes": 0})
    for t in textures:
        by_fmt[t["format"]]["count"] += 1
        by_fmt[t["format"]]["bytes"] += t["bytes"]
    print("=== By format ===")
    for fmt, info in sorted(by_fmt.items(), key=lambda kv: -kv[1]["bytes"]):
        print(f"  {fmt:40s} {info['count']:5d} textures  {info['bytes']/1024/1024:8.2f} MB")
    print()

    # Top 30 by bytes
    top = sorted(textures, key=lambda t: -t["bytes"])[:30]
    print("=== Top 30 by VRAM ===")
    print(f"  {'ID':>6s}  {'WxH':>14s}  mips  arr  {'format':40s} {'bind':30s} {'MB':>8s}")
    for t in top:
        dims = f"{t['width']}x{t['height']}"
        print(f"  {t['id']:>6s}  {dims:>14s}  {t['mips']:4d}  {t['array']:3d}  {t['format']:40s} {t['bind'][:30]:30s} {t['bytes']/1024/1024:8.2f}")

    if unknown:
        print()
        print("=== Unknown formats (treated as 0 bytes) ===")
        for fmt, n in sorted(unknown.items(), key=lambda kv: -kv[1]):
            print(f"  {fmt}: {n} textures")

if __name__ == "__main__":
    main()
