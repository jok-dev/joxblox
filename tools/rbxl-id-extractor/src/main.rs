use std::collections::BTreeMap;
use std::env;
use std::fs::{self, File};
use std::io::{BufReader, BufWriter, Cursor, Read};
use std::path::Path;
use std::sync::OnceLock;

use draco_decoder::decode_mesh_with_config_sync;
use flate2::read::GzDecoder;
use rbx_binary::from_reader;
use rbx_dom_weak::{Instance, WeakDom};
use rbx_types::{Content, ContentId, Variant};
use regex::Regex;
use serde::Serialize;

const RAW_ASSET_CONTEXT_WINDOW: usize = 80;
const PHYSICS_DATA_PROPERTY_TOKEN: &str = "physicsdata";
const ASSET_REFERENCE_PROPERTY_TOKENS: [&str; 15] = [
    "asset",
    "assetid",
    "texture",
    "image",
    "decal",
    "thumbnail",
    "shirt",
    "pants",
    "face",
    "icon",
    "content",
    "mesh",
    "sound",
    "animation",
    "font",
];

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct ExtractedAssetReference {
    id: i64,
    #[serde(skip_serializing_if = "String::is_empty")]
    raw_content: String,
    instance_type: String,
    instance_name: String,
    instance_path: String,
    property_name: String,
    used: usize,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    all_instance_paths: Vec<String>,
}

#[derive(Clone, Copy)]
struct WorldPosition {
    x: f32,
    y: f32,
    z: f32,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct PositionedAssetReference {
    id: i64,
    #[serde(skip_serializing_if = "String::is_empty")]
    raw_content: String,
    instance_type: String,
    instance_name: String,
    instance_path: String,
    property_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    world_x: Option<f32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    world_y: Option<f32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    world_z: Option<f32>,
}

#[derive(Clone)]
struct MaterialVariantBinding {
    base_material_key: String,
    references: Vec<ExtractedAssetReference>,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MapRenderPart {
    instance_type: String,
    instance_name: String,
    instance_path: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    material_key: String,
    center_x: Option<f32>,
    center_y: Option<f32>,
    center_z: Option<f32>,
    size_x: Option<f32>,
    size_y: Option<f32>,
    size_z: Option<f32>,
    basis_size_x: Option<f32>,
    basis_size_y: Option<f32>,
    basis_size_z: Option<f32>,
    yaw_degrees: Option<f32>,
    rotation_xx: Option<f32>,
    rotation_xy: Option<f32>,
    rotation_xz: Option<f32>,
    rotation_yx: Option<f32>,
    rotation_yy: Option<f32>,
    rotation_yz: Option<f32>,
    rotation_zx: Option<f32>,
    rotation_zy: Option<f32>,
    rotation_zz: Option<f32>,
    color_r: Option<u8>,
    color_g: Option<u8>,
    color_b: Option<u8>,
    transparency: Option<f32>,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MissingMaterialVariantReference {
    variant_name: String,
    instance_type: String,
    instance_name: String,
    instance_path: String,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MeshStatsResult {
    format_version: String,
    decoder_source: String,
    vertex_count: u32,
    triangle_count: u32,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MeshPreviewResult {
    format_version: String,
    decoder_source: String,
    vertex_count: u32,
    triangle_count: u32,
    preview_triangle_count: u32,
    positions: Vec<f32>,
    indices: Vec<u32>,
    /// Triangle ranges for each LOD into the `indices` array. LOD i covers
    /// triangles `[triangle_start .. triangle_end)`, i.e. indices
    /// `[triangle_start*3 .. triangle_end*3)`. Empty when no LOD metadata is
    /// available; always present with at least one entry when the mesh has
    /// any geometry.
    #[serde(default)]
    lods: Vec<MeshLodInfo>,
}

#[derive(Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
struct MeshLodInfo {
    triangle_start: u32,
    triangle_end: u32,
}

fn main() {
    if let Err(err) = run() {
        eprintln!("{}", err);
        std::process::exit(1);
    }
}

fn parse_path_prefixes(arg: &str) -> Vec<String> {
    arg.split(',')
        .map(|s| s.trim().to_ascii_lowercase())
        .filter(|s| !s.is_empty())
        .collect()
}

fn instance_matches_path_prefixes(instance_path: &str, prefixes: &[String]) -> bool {
    let normalized = instance_path.trim().to_ascii_lowercase();
    prefixes.iter().any(|prefix| normalized.starts_with(prefix))
}

fn run() -> Result<(), String> {
    let args: Vec<String> = env::args().collect();
    if args.len() >= 2 && args[1] == "mesh-stats" {
        return run_mesh_stats(&args);
    }
    if args.len() >= 2 && args[1] == "mesh-preview" {
        return run_mesh_preview(&args);
    }
    if args.len() >= 2 && args[1] == "map" {
        return run_map(&args);
    }
    if args.len() >= 2 && args[1] == "heatmap" {
        return run_heatmap(&args);
    }
    if args.len() >= 2 && args[1] == "material-warnings" {
        return run_material_warnings(&args);
    }
    if args.len() >= 2 && args[1] == "replace" {
        return run_replace(&args);
    }
    if args.len() < 2 {
        return Err(
            "usage: joxblox-rusty-asset-tool <rbxl-file> [max-results] [path-prefixes]\n       joxblox-rusty-asset-tool replace <input.rbxl> <output.rbxl> <replacements.json>".to_string(),
        );
    }

    let file_path = &args[1];
    let max_results = if args.len() >= 3 {
        let parsed_limit = args[2]
            .parse::<usize>()
            .map_err(|_| "max-results must be a non-negative integer".to_string())?;
        if parsed_limit == 0 {
            None
        } else {
            Some(parsed_limit)
        }
    } else {
        None
    };

    let path_prefixes: Vec<String> = if args.len() >= 4 {
        parse_path_prefixes(&args[3])
    } else {
        Vec::new()
    };
    let use_path_filter = !path_prefixes.is_empty();

    if use_path_filter {
        return run_filtered_extraction(file_path, max_results, &path_prefixes);
    }

    let file_bytes =
        fs::read(file_path).map_err(|read_err| format!("read failed: {}", read_err))?;
    let mut extracted_asset_references = BTreeMap::<String, ExtractedAssetReference>::new();
    let dom = match from_reader(BufReader::new(Cursor::new(&file_bytes))) {
        Ok(dom) => dom,
        Err(parse_err) => {
            if let Some(text_payload) = decode_supported_text_payload(&file_bytes) {
                let extracted_references =
                    extract_text_asset_references(&text_payload, file_path, max_results);
                let output = serde_json::to_string(&extracted_references)
                    .map_err(|json_err| format!("json failed: {}", json_err))?;
                println!("{}", output);
                return Ok(());
            }
            return Err(format!("parse failed: {}", parse_err));
        }
    };
    let material_variant_bindings = collect_material_variant_bindings(&dom, &[], false);
    for instance in dom.descendants() {
        let instance_type = instance.class.as_ref();
        let instance_name = instance.name.as_ref();
        let instance_path = build_instance_path(&dom, instance);
        let effective_references = collect_effective_asset_references(
            &dom,
            instance,
            instance_type,
            instance_name,
            &instance_path,
            &material_variant_bindings,
        );
        for reference in effective_references {
            record_asset_reference(
                &mut extracted_asset_references,
                reference.id,
                &reference.raw_content,
                &reference.instance_type,
                &reference.instance_name,
                &reference.instance_path,
                &reference.property_name,
            );
        }
    }

    let extracted_asset_references = if let Some(max_results) = max_results {
        extracted_asset_references
            .into_values()
            .take(max_results)
            .collect::<Vec<_>>()
    } else {
        extracted_asset_references.into_values().collect::<Vec<_>>()
    };
    let output = serde_json::to_string(&extracted_asset_references)
        .map_err(|json_err| format!("json failed: {}", json_err))?;
    println!("{}", output);
    Ok(())
}

fn run_mesh_stats(args: &[String]) -> Result<(), String> {
    if args.len() < 3 {
        return Err("usage: joxblox-rusty-asset-tool mesh-stats <mesh-file>".to_string());
    }

    let file_path = &args[2];
    let file_bytes = fs::read(file_path).map_err(|e| format!("read failed: {}", e))?;
    let stats = parse_mesh_stats(&file_bytes)?;
    let output = serde_json::to_string(&stats).map_err(|e| format!("json failed: {}", e))?;
    println!("{}", output);
    Ok(())
}

fn run_mesh_preview(args: &[String]) -> Result<(), String> {
    if args.len() < 3 {
        return Err(
            "usage: joxblox-rusty-asset-tool mesh-preview <mesh-file> [max-triangles]".to_string(),
        );
    }

    let file_path = &args[2];
    let max_triangles = if args.len() >= 4 {
        Some(
            args[3]
                .parse::<usize>()
                .map_err(|_| "max-triangles must be a non-negative integer".to_string())?,
        )
    } else {
        None
    };
    let file_bytes = fs::read(file_path).map_err(|e| format!("read failed: {}", e))?;
    let preview = parse_mesh_preview(&file_bytes, max_triangles)?;
    let output = serde_json::to_string(&preview).map_err(|e| format!("json failed: {}", e))?;
    println!("{}", output);
    Ok(())
}

fn parse_mesh_version_and_body(data: &[u8]) -> Result<(String, &[u8]), String> {
    if data.len() < 13 {
        return Err("mesh data too short".to_string());
    }
    let version_header =
        std::str::from_utf8(&data[..13]).map_err(|e| format!("invalid mesh header: {}", e))?;
    if !version_header.starts_with("version ") {
        return Err("not a Roblox mesh file".to_string());
    }
    let version = version_header["version ".len()..].trim().to_string();
    Ok((version, &data[13..]))
}

struct MeshGeometry {
    num_verts: u32,
    /// LOD0 face count (preserved for stats callers that only care about the
    /// highest-detail LOD).
    num_faces: u32,
    /// Total face count across every LOD combined.
    total_faces: u32,
    positions: Vec<f32>,
    /// Full index buffer covering every LOD (LOD0 first, then fallback LODs).
    indices: Vec<u32>,
    /// Cumulative triangle offsets read from the trailing LOD table, describing
    /// the per-LOD triangle ranges `[offsets[i] .. offsets[i + 1])`. Empty
    /// when no usable LOD table is present (treated as a single LOD by
    /// callers).
    lod_triangle_offsets: Vec<u32>,
}

/// Extract the first three f32 components (XYZ position) of each vertex out
/// of a variable-stride vertex buffer. The remaining vertex attributes
/// (normal, UV, tangent, colour, etc.) are ignored — the preview only needs
/// positions.
fn extract_mesh_positions(
    body: &[u8],
    vertex_start: usize,
    num_verts: u32,
    vertex_stride: usize,
    version: &str,
) -> Result<Vec<f32>, String> {
    const POSITION_BYTES: usize = 12;
    if vertex_stride < POSITION_BYTES {
        return Err(format!(
            "v{} mesh vertex stride {} is smaller than the 12-byte XYZ position",
            version, vertex_stride,
        ));
    }
    let mut positions = Vec::with_capacity(num_verts as usize * 3);
    for i in 0..num_verts as usize {
        let off = vertex_start + i * vertex_stride;
        let x = f32::from_le_bytes(body[off..off + 4].try_into().unwrap());
        let y = f32::from_le_bytes(body[off + 4..off + 8].try_into().unwrap());
        let z = f32::from_le_bytes(body[off + 8..off + 12].try_into().unwrap());
        positions.push(x);
        positions.push(y);
        positions.push(z);
    }
    Ok(positions)
}

/// Extract the first three u32 indices of each face out of a variable-stride
/// face buffer. Any trailing face metadata (material ids, etc.) is ignored.
fn extract_mesh_indices(
    body: &[u8],
    face_start: usize,
    num_faces: u32,
    face_stride: usize,
    version: &str,
) -> Result<Vec<u32>, String> {
    const INDEX_BYTES: usize = 12;
    if face_stride < INDEX_BYTES {
        return Err(format!(
            "v{} mesh face stride {} is smaller than the 12-byte index triplet",
            version, face_stride,
        ));
    }
    let mut indices = Vec::with_capacity(num_faces as usize * 3);
    for i in 0..num_faces as usize {
        let off = face_start + i * face_stride;
        let a = u32::from_le_bytes(body[off..off + 4].try_into().unwrap());
        let b = u32::from_le_bytes(body[off + 4..off + 8].try_into().unwrap());
        let c = u32::from_le_bytes(body[off + 8..off + 12].try_into().unwrap());
        indices.push(a);
        indices.push(b);
        indices.push(c);
    }
    Ok(indices)
}

/// Parse a trailing LOD offset table (shared between v3 and v4/v5): a sequence
/// of `num_lod_offsets` u32 values representing cumulative face-end
/// boundaries. Returns an empty vector when the table is absent, truncated,
/// or fails sanity checks — callers treat that as a single-LOD mesh.
fn parse_mesh_lod_offset_table(
    body: &[u8],
    lod_table_start: usize,
    num_lod_offsets: usize,
    num_faces: u32,
) -> Vec<u32> {
    if num_lod_offsets < 2 {
        return Vec::new();
    }
    let lod_table_byte_count = num_lod_offsets.saturating_mul(4);
    if body.len() < lod_table_start + lod_table_byte_count {
        return Vec::new();
    }
    let mut offsets = Vec::with_capacity(num_lod_offsets);
    for i in 0..num_lod_offsets {
        let off = lod_table_start + i * 4;
        let value = u32::from_le_bytes(body[off..off + 4].try_into().unwrap());
        offsets.push(value);
    }
    let trusted = offsets[0] == 0
        && offsets.windows(2).all(|w| w[0] <= w[1])
        && *offsets.last().unwrap() <= num_faces;
    if trusted {
        offsets
    } else {
        Vec::new()
    }
}

fn parse_v3_mesh_body(body: &[u8], version: &str) -> Result<MeshGeometry, String> {
    // v3 mesh header layout (body starts immediately after the
    // `version 3.xx\n` prefix):
    //   +0..+1 : u16 sizeof_header (absolute offset where vertex data starts)
    //   +2     : u8  sizeof_vertex (variable, typically 36 or 40 bytes)
    //   +3     : u8  sizeof_face   (variable, typically 12 bytes)
    //   +4..+5 : u16 unknown / padding
    //   +6..+7 : u16 num_lod_offsets
    //   +8..+11: u32 num_verts
    //   +12..15: u32 num_faces
    if body.len() < 16 {
        return Err(format!("v{} mesh body too short for header", version));
    }
    let sizeof_header = u16::from_le_bytes([body[0], body[1]]) as usize;
    let sizeof_vertex = body[2] as usize;
    let sizeof_face = body[3] as usize;
    let num_lod_offsets = u16::from_le_bytes([body[6], body[7]]) as usize;
    let num_verts = u32::from_le_bytes([body[8], body[9], body[10], body[11]]);
    let num_faces = u32::from_le_bytes([body[12], body[13], body[14], body[15]]);

    if sizeof_header == 0 {
        return Err(format!("v{} mesh sizeof_header is zero", version));
    }
    if sizeof_vertex == 0 || sizeof_face == 0 {
        return Err(format!("v{} mesh reports zero-byte vertex or face stride", version));
    }

    let vertex_start = sizeof_header;
    let vertex_end = vertex_start + (num_verts as usize) * sizeof_vertex;
    let face_start = vertex_end;
    let face_end = face_start + (num_faces as usize) * sizeof_face;
    if body.len() < face_end {
        return Err(format!(
            "v{} mesh data truncated: need {} bytes, have {}",
            version,
            face_end,
            body.len()
        ));
    }

    let lod_triangle_offsets =
        parse_mesh_lod_offset_table(body, face_end, num_lod_offsets, num_faces);
    let lod0_faces = lod_triangle_offsets
        .get(1)
        .copied()
        .filter(|&value| value > 0 && value <= num_faces)
        .unwrap_or(num_faces);

    let positions = extract_mesh_positions(body, vertex_start, num_verts, sizeof_vertex, version)?;
    let indices = extract_mesh_indices(body, face_start, num_faces, sizeof_face, version)?;

    Ok(MeshGeometry {
        num_verts,
        num_faces: lod0_faces,
        total_faces: num_faces,
        positions,
        indices,
        lod_triangle_offsets,
    })
}

fn parse_v4_mesh_body(body: &[u8], version: &str) -> Result<MeshGeometry, String> {
    if body.len() < 16 {
        return Err(format!("v{} mesh body too short for header", version));
    }
    let sizeof_header = u16::from_le_bytes([body[0], body[1]]) as usize;
    let num_verts = u32::from_le_bytes([body[4], body[5], body[6], body[7]]);
    let num_faces = u32::from_le_bytes([body[8], body[9], body[10], body[11]]);
    let num_lod_offsets = u16::from_le_bytes([body[12], body[13]]) as usize;
    let num_bones = u16::from_le_bytes([body[14], body[15]]) as usize;

    const VERTEX_STRIDE: usize = 40;
    const FACE_STRIDE: usize = 12;
    let skinning_stride: usize = if num_bones > 0 { 8 } else { 0 };
    let skinning_size = (num_verts as usize) * skinning_stride;

    let vertex_start = sizeof_header;
    let vertex_end = vertex_start + (num_verts as usize) * VERTEX_STRIDE;
    let face_start = vertex_end + skinning_size;
    let face_end = face_start + (num_faces as usize) * FACE_STRIDE;

    if body.len() < face_end {
        return Err(format!(
            "v{} mesh data truncated: need {} bytes, have {}",
            version,
            face_end,
            body.len()
        ));
    }

    // v4+ gates the LOD table behind a minimum header size (24 bytes) to
    // distinguish it from older variants that reused the same preamble but
    // did not write the table; v3 does not need this check because the LOD
    // offset count itself is enough.
    let lod_triangle_offsets = if sizeof_header >= 24 {
        parse_mesh_lod_offset_table(body, face_end, num_lod_offsets, num_faces)
    } else {
        Vec::new()
    };
    let lod0_faces = lod_triangle_offsets
        .get(1)
        .copied()
        .filter(|&value| value > 0 && value <= num_faces)
        .unwrap_or(num_faces);

    let positions = extract_mesh_positions(body, vertex_start, num_verts, VERTEX_STRIDE, version)?;
    let indices = extract_mesh_indices(body, face_start, num_faces, FACE_STRIDE, version)?;

    Ok(MeshGeometry {
        num_verts,
        num_faces: lod0_faces,
        total_faces: num_faces,
        positions,
        indices,
        lod_triangle_offsets,
    })
}

fn parse_mesh_stats(data: &[u8]) -> Result<MeshStatsResult, String> {
    let (version, body) = parse_mesh_version_and_body(data)?;

    if version == "7.00" && body.starts_with(b"COREMESH") {
        // v7.00 CoreMesh files concatenate every LOD into one geometry buffer
        // and describe them with a "LODS" footer at the tail. The Draco
        // decoder returns triangles for every LOD combined; for triangle
        // counting we only want LOD0 (the highest-detail mesh that Roblox
        // renders at close range), so we consult the LODS footer when
        // available.
        if let Some(draco_start) = find_draco_payload_start(body) {
            let draco_payload = &body[draco_start..];
            let decode_result = decode_mesh_with_config_sync(draco_payload)
                .ok_or_else(|| "Draco decode failed".to_string())?;
            let vertex_count = decode_result.config.vertex_count() as u32;
            let draco_triangle_total = (decode_result.config.index_count() / 3) as u32;
            let triangle_count = parse_v7_lod0_triangle_count(body).unwrap_or(draco_triangle_total);
            return Ok(MeshStatsResult {
                format_version: version,
                decoder_source: "draco".to_string(),
                vertex_count,
                triangle_count,
            });
        }

        // Uncompressed COREMESH: header followed by vertex + face arrays.
        let uncompressed = parse_v7_uncompressed_mesh_body(body, &version)?;
        return Ok(MeshStatsResult {
            format_version: version,
            decoder_source: "binary".to_string(),
            vertex_count: uncompressed.num_verts,
            triangle_count: uncompressed.num_faces,
        });
    }

    if matches!(version.as_str(), "4.00" | "4.01" | "5.00" | "6.00") {
        let mesh = parse_v4_mesh_body(body, &version)?;
        return Ok(MeshStatsResult {
            format_version: version,
            decoder_source: "binary".to_string(),
            vertex_count: mesh.num_verts,
            triangle_count: mesh.num_faces,
        });
    }

    if matches!(version.as_str(), "3.00" | "3.01") {
        let mesh = parse_v3_mesh_body(body, &version)?;
        return Ok(MeshStatsResult {
            format_version: version,
            decoder_source: "binary".to_string(),
            vertex_count: mesh.num_verts,
            triangle_count: mesh.num_faces,
        });
    }

    if version == "2.00" {
        let mesh = parse_v2_mesh_body(body, &version)?;
        return Ok(MeshStatsResult {
            format_version: version,
            decoder_source: "binary".to_string(),
            vertex_count: mesh.num_verts,
            triangle_count: mesh.num_faces,
        });
    }

    if matches!(version.as_str(), "1.00" | "1.01") {
        let mesh = parse_v1_mesh_body(body, &version)?;
        return Ok(MeshStatsResult {
            format_version: version,
            decoder_source: "text".to_string(),
            vertex_count: mesh.num_verts,
            triangle_count: mesh.num_faces,
        });
    }

    Err(format!(
        "unsupported mesh stats format: version {}",
        version
    ))
}

/// Parse a v7.00 uncompressed CoreMesh body into a full [`MeshGeometry`].
///
/// Layout (after the `COREMESH` marker):
/// ```text
///   +0..+15  header (16 bytes of metadata we don't use for geometry)
///   +16..+19 u32 num_verts
///   +20..    num_verts × 40-byte vertices  (first 12 bytes = XYZ position)
///   ...      u32 num_faces
///   ...      num_faces × 12-byte indices   (3 × u32 per face)
///   tail     optional "LODS" footer describing cumulative per-LOD offsets
/// ```
fn parse_v7_uncompressed_mesh_body(body: &[u8], version: &str) -> Result<MeshGeometry, String> {
    if body.len() < 20 {
        return Err("v7 uncompressed COREMESH header truncated".to_string());
    }
    let num_verts = u32::from_le_bytes(body[16..20].try_into().unwrap());

    const VERTEX_STRIDE: usize = 40;
    const FACE_STRIDE: usize = 12;

    let vertex_start = 20usize;
    let vertex_end = vertex_start
        .checked_add((num_verts as usize).saturating_mul(VERTEX_STRIDE))
        .ok_or_else(|| "v7 uncompressed vertex range overflow".to_string())?;
    if body.len() < vertex_end + 4 {
        return Err("v7 uncompressed COREMESH faces truncated".to_string());
    }
    let num_faces = u32::from_le_bytes(body[vertex_end..vertex_end + 4].try_into().unwrap());
    let face_start = vertex_end + 4;
    let face_end = face_start
        .checked_add((num_faces as usize).saturating_mul(FACE_STRIDE))
        .ok_or_else(|| "v7 uncompressed face range overflow".to_string())?;
    if body.len() < face_end {
        return Err(format!(
            "v7 uncompressed COREMESH face data truncated: need {} bytes, have {}",
            face_end,
            body.len()
        ));
    }

    let positions = extract_mesh_positions(body, vertex_start, num_verts, VERTEX_STRIDE, version)?;
    let indices = extract_mesh_indices(body, face_start, num_faces, FACE_STRIDE, version)?;

    // Re-use the v7 CoreMesh LODS footer parser: uncompressed files use the
    // exact same trailing footer when multi-LOD data is present.
    let lod_triangle_offsets = parse_v7_lod_triangle_offsets(body).unwrap_or_default();
    let lod0_faces = lod_triangle_offsets
        .get(1)
        .copied()
        .filter(|&value| value > 0 && value <= num_faces)
        .unwrap_or(num_faces);

    Ok(MeshGeometry {
        num_verts,
        num_faces: lod0_faces,
        total_faces: num_faces,
        positions,
        indices,
        lod_triangle_offsets,
    })
}

fn parse_v2_mesh_body(body: &[u8], version: &str) -> Result<MeshGeometry, String> {
    // v2 binary layout: u16 sizeof_header, u8 sizeof_vertex, u8 sizeof_face,
    // u32 num_verts, u32 num_faces, then vertex + face arrays. v2 does not
    // ship an LOD offset table — the mesh is always single-LOD.
    if body.len() < 12 {
        return Err(format!("v{} mesh header truncated", version));
    }
    let sizeof_header = u16::from_le_bytes([body[0], body[1]]) as usize;
    let sizeof_vertex = body[2] as usize;
    let sizeof_face = body[3] as usize;
    let num_verts = u32::from_le_bytes([body[4], body[5], body[6], body[7]]);
    let num_faces = u32::from_le_bytes([body[8], body[9], body[10], body[11]]);

    if sizeof_header < 12 {
        return Err(format!(
            "v{} mesh header size too small: {}",
            version, sizeof_header
        ));
    }
    if sizeof_vertex == 0 || sizeof_face == 0 {
        return Err(format!(
            "v{} mesh reports zero-byte vertex or face stride",
            version
        ));
    }

    let vertex_start = sizeof_header;
    let vertex_end = vertex_start + (num_verts as usize) * sizeof_vertex;
    let face_start = vertex_end;
    let face_end = face_start + (num_faces as usize) * sizeof_face;
    if body.len() < face_end {
        return Err(format!(
            "v{} mesh data truncated: need {} bytes, have {}",
            version,
            face_end,
            body.len()
        ));
    }

    let positions = extract_mesh_positions(body, vertex_start, num_verts, sizeof_vertex, version)?;
    let indices = extract_mesh_indices(body, face_start, num_faces, sizeof_face, version)?;

    Ok(MeshGeometry {
        num_verts,
        num_faces,
        total_faces: num_faces,
        positions,
        indices,
        lod_triangle_offsets: Vec::new(),
    })
}

/// Parse a v1.00 / v1.01 ASCII mesh body.
///
/// The body layout is:
/// ```text
///   num_faces\n
///   [x,y,z][nx,ny,nz][u,v,w]\n      (repeated 3 * num_faces times; one line per vertex)
/// ```
///
/// v1 meshes do not share vertices between triangles and have no index
/// buffer — every triangle has three freshly-listed vertices. We synthesise
/// the index buffer as `[0, 1, 2, 3, ...]` so downstream preview code can
/// treat the result uniformly.
fn parse_v1_mesh_body(body: &[u8], version: &str) -> Result<MeshGeometry, String> {
    let text = std::str::from_utf8(body)
        .map_err(|err| format!("v{} mesh body is not UTF-8: {}", version, err))?;
    let mut lines = text.lines();

    let num_faces_line = lines
        .next()
        .ok_or_else(|| format!("v{} mesh missing face count line", version))?;
    let num_faces: u32 = num_faces_line.trim().parse().map_err(|err| {
        format!(
            "v{} mesh invalid face count \"{}\": {}",
            version,
            num_faces_line.trim(),
            err
        )
    })?;
    let expected_vertices = (num_faces as usize).saturating_mul(3);

    let mut positions = Vec::with_capacity(expected_vertices * 3);
    let mut vertex_count = 0usize;
    for line in lines {
        if vertex_count >= expected_vertices {
            break;
        }
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }
        let position = parse_v1_vertex_position(trimmed).map_err(|err| {
            format!("v{} mesh malformed vertex line {:?}: {}", version, trimmed, err)
        })?;
        positions.extend_from_slice(&position);
        vertex_count += 1;
    }

    if vertex_count < expected_vertices {
        return Err(format!(
            "v{} mesh has {} vertex lines, expected {} (= 3 × {} faces)",
            version, vertex_count, expected_vertices, num_faces
        ));
    }

    let indices: Vec<u32> = (0..expected_vertices as u32).collect();

    Ok(MeshGeometry {
        num_verts: expected_vertices as u32,
        num_faces,
        total_faces: num_faces,
        positions,
        indices,
        lod_triangle_offsets: Vec::new(),
    })
}

/// Extract the `[x,y,z]` position tuple from a v1 vertex line. Subsequent
/// `[normal]` and `[uv]` groups on the line are ignored — the preview only
/// uses positions.
fn parse_v1_vertex_position(line: &str) -> Result<[f32; 3], String> {
    let open = line
        .find('[')
        .ok_or_else(|| "missing '[' before position tuple".to_string())?;
    let close = line[open..]
        .find(']')
        .ok_or_else(|| "missing ']' closing position tuple".to_string())?;
    let body = &line[open + 1..open + close];
    let parts: Vec<&str> = body.split(',').collect();
    if parts.len() != 3 {
        return Err(format!(
            "position tuple has {} components, expected 3",
            parts.len()
        ));
    }
    let mut result = [0f32; 3];
    for (i, part) in parts.iter().enumerate() {
        result[i] = part
            .trim()
            .parse()
            .map_err(|err| format!("component {} parse error: {}", i, err))?;
    }
    Ok(result)
}

// Parse the trailing "LODS" footer of a v7.00 CoreMesh body and return the
// cumulative triangle offsets (length `num_lods + 1`) that describe the
// triangle range for each LOD. LOD `i` covers triangles
// `[offsets[i] .. offsets[i + 1])`. Observed footer layout:
//   +0..+3   : "LODS"
//   +4..+7   : u32 reserved (0)
//   +8..+11  : u32 unknown (typically 1)
//   +12..+15 : u32 header size (typically 31)
//   +16..+17 : u16 num_lods
//   +18..+22 : 5 bytes of flags / render-fidelity metadata
//   +23..    : (num_lods + 1) little-endian u32 values -- cumulative triangle
//              end offsets for each LOD.
fn parse_v7_lod_triangle_offsets(body: &[u8]) -> Option<Vec<u32>> {
    let lods_off = find_lods_marker(body)?;
    let tail = &body[lods_off..];
    if tail.len() < 31 {
        return None;
    }
    let num_lods = u16::from_le_bytes(tail[16..18].try_into().ok()?) as usize;
    if num_lods == 0 || num_lods > 16 {
        return None;
    }
    const ARRAY_OFFSET: usize = 23;
    let required_len = ARRAY_OFFSET + (num_lods + 1) * 4;
    if tail.len() < required_len {
        return None;
    }
    let mut offsets = Vec::with_capacity(num_lods + 1);
    for i in 0..=num_lods {
        let start = ARRAY_OFFSET + i * 4;
        let value = u32::from_le_bytes(tail[start..start + 4].try_into().ok()?);
        offsets.push(value);
    }
    if offsets[0] != 0 {
        return None;
    }
    // Offsets should be monotonically non-decreasing.
    if offsets.windows(2).any(|w| w[0] > w[1]) {
        return None;
    }
    Some(offsets)
}

fn parse_v7_lod0_triangle_count(body: &[u8]) -> Option<u32> {
    parse_v7_lod_triangle_offsets(body).and_then(|offsets| {
        let end = *offsets.get(1)?;
        if end == 0 {
            None
        } else {
            Some(end)
        }
    })
}

fn triangle_offsets_to_lods(offsets: &[u32], max_triangles: u32) -> Vec<MeshLodInfo> {
    let mut lods = Vec::new();
    for pair in offsets.windows(2) {
        let start = pair[0].min(max_triangles);
        let mut end = pair[1].min(max_triangles);
        if end < start {
            end = start;
        }
        // Drop clearly empty trailing LOD entries that result from a
        // truncated mesh payload. Always keep at least one entry.
        if start == end && !lods.is_empty() {
            continue;
        }
        lods.push(MeshLodInfo {
            triangle_start: start,
            triangle_end: end,
        });
    }
    lods
}

fn find_lods_marker(body: &[u8]) -> Option<usize> {
    // Scan from the tail of the file -- the LODS footer lives at the very end.
    if body.len() < 4 {
        return None;
    }
    let mut i = body.len() - 4;
    loop {
        if &body[i..i + 4] == b"LODS" {
            return Some(i);
        }
        if i == 0 {
            return None;
        }
        i -= 1;
    }
}

fn parse_mesh_preview(
    data: &[u8],
    max_triangles: Option<usize>,
) -> Result<MeshPreviewResult, String> {
    let (version, body) = parse_mesh_version_and_body(data)?;

    if version == "7.00" && body.starts_with(b"COREMESH") {
        if let Some(draco_start) = find_draco_payload_start(body) {
            let draco_payload = &body[draco_start..];
            let decode_result = decode_mesh_with_config_sync(draco_payload)
                .ok_or_else(|| "Draco decode failed".to_string())?;
            let positions = extract_position_components(&decode_result)?;
            let all_indices = extract_triangle_indices(&decode_result)?;
            let total_triangles = (all_indices.len() / 3) as u32;
            let lods = build_lods_for_preview(
                parse_v7_lod_triangle_offsets(body),
                total_triangles,
                max_triangles,
            );
            let preview_indices = limit_triangle_indices(&all_indices, max_triangles);
            return Ok(MeshPreviewResult {
                format_version: version,
                decoder_source: "draco".to_string(),
                vertex_count: decode_result.config.vertex_count(),
                triangle_count: total_triangles,
                preview_triangle_count: (preview_indices.len() / 3) as u32,
                positions,
                indices: preview_indices,
                lods,
            });
        }

        // Uncompressed v7 CoreMesh: fall through to the binary parser so we
        // can still preview meshes whose payload is not Draco-encoded.
        let mesh = parse_v7_uncompressed_mesh_body(body, &version)?;
        return Ok(mesh_geometry_to_preview(version, mesh, max_triangles));
    }

    if matches!(version.as_str(), "4.00" | "4.01" | "5.00" | "6.00") {
        // v6.00 is extremely rare in the wild; its header is structurally
        // identical to v4/v5 (u16 sizeof_header + u32 num_verts + u32
        // num_faces + ...), so we route it through the same parser. If real
        // v6 samples turn out to diverge we'll iterate here with a dedicated
        // parser.
        let mesh = parse_v4_mesh_body(body, &version)?;
        return Ok(mesh_geometry_to_preview(version, mesh, max_triangles));
    }

    if matches!(version.as_str(), "3.00" | "3.01") {
        let mesh = parse_v3_mesh_body(body, &version)?;
        return Ok(mesh_geometry_to_preview(version, mesh, max_triangles));
    }

    if version == "2.00" {
        let mesh = parse_v2_mesh_body(body, &version)?;
        return Ok(mesh_geometry_to_preview(version, mesh, max_triangles));
    }

    if matches!(version.as_str(), "1.00" | "1.01") {
        let mesh = parse_v1_mesh_body(body, &version)?;
        let mut preview = mesh_geometry_to_preview(version, mesh, max_triangles);
        preview.decoder_source = "text".to_string();
        return Ok(preview);
    }

    Err(format!(
        "unsupported mesh preview format: version {}",
        version
    ))
}

/// Shared adapter that converts a fully-decoded [`MeshGeometry`] (produced by
/// the v3/v4/v5 binary parsers) into a [`MeshPreviewResult`].
fn mesh_geometry_to_preview(
    version: String,
    mesh: MeshGeometry,
    max_triangles: Option<usize>,
) -> MeshPreviewResult {
    let total_triangles = mesh.total_faces;
    let lod_offsets = if mesh.lod_triangle_offsets.is_empty() {
        None
    } else {
        Some(mesh.lod_triangle_offsets.clone())
    };
    let lods = build_lods_for_preview(lod_offsets, total_triangles, max_triangles);
    let preview_indices = limit_triangle_indices(&mesh.indices, max_triangles);
    MeshPreviewResult {
        format_version: version,
        decoder_source: "binary".to_string(),
        vertex_count: mesh.num_verts,
        triangle_count: total_triangles,
        preview_triangle_count: (preview_indices.len() / 3) as u32,
        positions: mesh.positions,
        indices: preview_indices,
        lods,
    }
}

// `lods` always reflects the real per-LOD triangle ranges from the mesh file
// (not clipped by `max_triangles`) so callers can display accurate triangle
// counts. Callers that intend to render a specific LOD from the returned
// `indices` slice must request an unlimited preview, because a capped
// preview sub-samples across the whole mesh and is no longer sliceable by
// LOD range.
fn build_lods_for_preview(
    lod_triangle_offsets: Option<Vec<u32>>,
    total_triangles: u32,
    _max_triangles: Option<usize>,
) -> Vec<MeshLodInfo> {
    let offsets = match lod_triangle_offsets {
        Some(mut offsets) if offsets.len() >= 2 => {
            if let Some(last) = offsets.last_mut() {
                if *last > total_triangles {
                    *last = total_triangles;
                }
            }
            offsets
        }
        _ => vec![0, total_triangles],
    };
    triangle_offsets_to_lods(&offsets, total_triangles)
}

fn extract_position_components(
    decode_result: &draco_decoder::MeshDecodeResult,
) -> Result<Vec<f32>, String> {
    let vertex_count = decode_result.config.vertex_count() as usize;
    let position_attribute = decode_result
        .config
        .attributes()
        .into_iter()
        .find(|attribute| {
            attribute.data_type() == draco_decoder::AttributeDataType::Float32
                && attribute.dim() >= 3
        })
        .ok_or_else(|| "missing Float32 position attribute".to_string())?;

    let start = position_attribute.offset() as usize;
    let component_count = vertex_count
        .checked_mul(position_attribute.dim() as usize)
        .ok_or_else(|| "position component count overflow".to_string())?;
    let byte_count = component_count
        .checked_mul(std::mem::size_of::<f32>())
        .ok_or_else(|| "position byte count overflow".to_string())?;
    let end = start
        .checked_add(byte_count)
        .ok_or_else(|| "position buffer range overflow".to_string())?;
    if end > decode_result.data.len() {
        return Err("position buffer truncated".to_string());
    }

    let mut positions = Vec::with_capacity(vertex_count * 3);
    let stride = position_attribute.dim() as usize * std::mem::size_of::<f32>();
    for vertex_index in 0..vertex_count {
        let vertex_start = start + vertex_index * stride;
        let vertex_end = vertex_start + stride;
        if vertex_end > decode_result.data.len() {
            return Err("position vertex buffer truncated".to_string());
        }
        for component_index in 0..3 {
            let component_start = vertex_start + component_index * 4;
            let component_end = component_start + 4;
            let bytes = decode_result.data[component_start..component_end]
                .try_into()
                .map_err(|_| "position component decode failed".to_string())?;
            positions.push(f32::from_le_bytes(bytes));
        }
    }
    Ok(positions)
}

fn extract_triangle_indices(
    decode_result: &draco_decoder::MeshDecodeResult,
) -> Result<Vec<u32>, String> {
    let index_count = decode_result.config.index_count() as usize;
    let index_length = decode_result.config.index_length() as usize;
    if index_length > decode_result.data.len() {
        return Err("index buffer truncated".to_string());
    }
    let index_bytes = &decode_result.data[..index_length];
    let mut indices = Vec::with_capacity(index_count);
    if index_length == index_count * 2 {
        for chunk in index_bytes.chunks_exact(2) {
            let bytes: [u8; 2] = chunk
                .try_into()
                .map_err(|_| "u16 index decode failed".to_string())?;
            indices.push(u16::from_le_bytes(bytes) as u32);
        }
    } else if index_length == index_count * 4 {
        for chunk in index_bytes.chunks_exact(4) {
            let bytes: [u8; 4] = chunk
                .try_into()
                .map_err(|_| "u32 index decode failed".to_string())?;
            indices.push(u32::from_le_bytes(bytes));
        }
    } else {
        return Err("unsupported Draco index buffer layout".to_string());
    }
    Ok(indices)
}

fn limit_triangle_indices(indices: &[u32], max_triangles: Option<usize>) -> Vec<u32> {
    let Some(max_triangles) = max_triangles else {
        return indices.to_vec();
    };
    if max_triangles == 0 {
        return Vec::new();
    }
    let total_triangles = indices.len() / 3;
    if total_triangles <= max_triangles {
        return indices.to_vec();
    }

    let stride = total_triangles as f64 / max_triangles as f64;
    let mut limited = Vec::with_capacity(max_triangles * 3);
    for sampled_triangle in 0..max_triangles {
        let triangle_index = ((sampled_triangle as f64) * stride).floor() as usize;
        let base = triangle_index.min(total_triangles.saturating_sub(1)) * 3;
        limited.extend_from_slice(&indices[base..base + 3]);
    }
    limited
}

fn find_draco_payload_start(body: &[u8]) -> Option<usize> {
    body.windows(5).position(|window| window == b"DRACO")
}

fn run_map(args: &[String]) -> Result<(), String> {
    if args.len() < 3 {
        return Err("usage: joxblox-rusty-asset-tool map <rbxl-file> [path-prefixes]".to_string());
    }

    let file_path = &args[2];
    let path_prefixes: Vec<String> = if args.len() >= 4 {
        parse_path_prefixes(&args[3])
    } else {
        Vec::new()
    };
    let use_path_filter = !path_prefixes.is_empty();

    let file = File::open(file_path).map_err(|open_err| format!("open failed: {}", open_err))?;
    let dom = from_reader(BufReader::new(file)).map_err(|e| format!("parse failed: {}", e))?;
    let mut parts: Vec<MapRenderPart> = Vec::new();

    for instance in dom.descendants() {
        let instance_path = build_instance_path(&dom, instance);
        if use_path_filter && !instance_matches_path_prefixes(&instance_path, &path_prefixes) {
            continue;
        }
        if let Some(map_part) = extract_map_render_part(instance, &instance_path) {
            parts.push(map_part);
        }
    }

    let output =
        serde_json::to_string(&parts).map_err(|json_err| format!("json failed: {}", json_err))?;
    println!("{}", output);
    Ok(())
}

fn run_heatmap(args: &[String]) -> Result<(), String> {
    if args.len() < 3 {
        return Err(
            "usage: joxblox-rusty-asset-tool heatmap <rbxl-file> [path-prefixes]".to_string(),
        );
    }

    let file_path = &args[2];
    let path_prefixes: Vec<String> = if args.len() >= 4 {
        parse_path_prefixes(&args[3])
    } else {
        Vec::new()
    };
    let use_path_filter = !path_prefixes.is_empty();

    let file = File::open(file_path).map_err(|open_err| format!("open failed: {}", open_err))?;
    let dom = from_reader(BufReader::new(file)).map_err(|e| format!("parse failed: {}", e))?;
    let mut references: Vec<PositionedAssetReference> = Vec::new();
    let material_variant_bindings =
        collect_material_variant_bindings(&dom, &path_prefixes, use_path_filter);

    for instance in dom.descendants() {
        let instance_path = build_instance_path(&dom, instance);
        if use_path_filter && !instance_matches_path_prefixes(&instance_path, &path_prefixes) {
            continue;
        }

        let instance_type = instance.class.as_ref();
        let instance_name = instance.name.as_ref();
        let world_position = resolve_instance_world_position(&dom, instance.referent());
        if normalize_for_match(instance_type) != "materialvariant" {
            append_positioned_instance_asset_references(
                &dom,
                instance,
                &mut references,
                instance_type,
                instance_name,
                &instance_path,
                world_position,
            );
        }
        if let Some(material_variant_binding) =
            resolve_material_variant_binding(instance, &material_variant_bindings)
        {
            append_positioned_material_variant_references(
                material_variant_binding,
                &mut references,
                instance_type,
                instance_name,
                &instance_path,
                world_position,
            );
        }
    }

    let output = serde_json::to_string(&references)
        .map_err(|json_err| format!("json failed: {}", json_err))?;
    println!("{}", output);
    Ok(())
}

fn run_material_warnings(args: &[String]) -> Result<(), String> {
    if args.len() < 3 {
        return Err(
            "usage: joxblox-rusty-asset-tool material-warnings <rbxl-file> [path-prefixes]"
                .to_string(),
        );
    }

    let file_path = &args[2];
    let path_prefixes: Vec<String> = if args.len() >= 4 {
        parse_path_prefixes(&args[3])
    } else {
        Vec::new()
    };
    let use_path_filter = !path_prefixes.is_empty();

    let file = File::open(file_path).map_err(|open_err| format!("open failed: {}", open_err))?;
    let dom = from_reader(BufReader::new(file)).map_err(|e| format!("parse failed: {}", e))?;
    let warnings =
        collect_missing_material_variant_references(&dom, &path_prefixes, use_path_filter);
    let output = serde_json::to_string(&warnings)
        .map_err(|json_err| format!("json failed: {}", json_err))?;
    println!("{}", output);
    Ok(())
}

fn run_filtered_extraction(
    file_path: &str,
    max_results: Option<usize>,
    path_prefixes: &[String],
) -> Result<(), String> {
    let file = File::open(file_path).map_err(|open_err| format!("open failed: {}", open_err))?;
    let dom = from_reader(BufReader::new(file)).map_err(|e| format!("parse failed: {}", e))?;

    let mut all_references: Vec<ExtractedAssetReference> = Vec::new();
    let material_variant_bindings = collect_material_variant_bindings(&dom, path_prefixes, true);

    for instance in dom.descendants() {
        let instance_path = build_instance_path(&dom, instance);
        if !instance_matches_path_prefixes(&instance_path, path_prefixes) {
            continue;
        }

        let instance_type = instance.class.as_ref();
        let instance_name = instance.name.as_ref();
        all_references.extend(collect_effective_asset_references(
            &dom,
            instance,
            instance_type,
            instance_name,
            &instance_path,
            &material_variant_bindings,
        ));
    }

    if let Some(max) = max_results {
        all_references.truncate(max);
    }
    let output = serde_json::to_string(&all_references)
        .map_err(|json_err| format!("json failed: {}", json_err))?;
    println!("{}", output);
    Ok(())
}

fn decode_supported_text_payload(file_bytes: &[u8]) -> Option<String> {
    if file_bytes.is_empty() {
        return None;
    }

    if let Some(decoded_gzip) = decode_gzip_payload(file_bytes) {
        if looks_like_supported_text_payload(&decoded_gzip) {
            return Some(decoded_gzip);
        }
    }

    let decoded_text = String::from_utf8_lossy(file_bytes).into_owned();
    if looks_like_supported_text_payload(&decoded_text) {
        return Some(decoded_text);
    }

    None
}

fn decode_gzip_payload(file_bytes: &[u8]) -> Option<String> {
    if file_bytes.len() < 2 || file_bytes[0] != 0x1f || file_bytes[1] != 0x8b {
        return None;
    }

    let mut decoder = GzDecoder::new(Cursor::new(file_bytes));
    let mut decoded_text = String::new();
    if decoder.read_to_string(&mut decoded_text).is_ok() {
        return Some(decoded_text);
    }
    None
}

fn looks_like_supported_text_payload(text: &str) -> bool {
    let trimmed_text = text.trim_start();
    let normalized = trimmed_text.to_ascii_lowercase();
    normalized.starts_with("<roblox")
        || normalized.starts_with("<?xml")
        || normalized.contains("<texturepack_version>")
}

fn extract_text_asset_references(
    text_payload: &str,
    file_path: &str,
    max_results: Option<usize>,
) -> Vec<ExtractedAssetReference> {
    let mut extracted_asset_references = BTreeMap::<String, ExtractedAssetReference>::new();
    let instance_name = Path::new(file_path)
        .file_name()
        .and_then(|value| value.to_str())
        .unwrap_or("text-asset");
    extract_ids_from_text(
        text_payload,
        &mut extracted_asset_references,
        "TextAsset",
        instance_name,
        instance_name,
        "content",
    );

    let mut extracted_references = extracted_asset_references.into_values().collect::<Vec<_>>();
    if let Some(max_results) = max_results {
        extracted_references.truncate(max_results);
    }
    extracted_references
}

fn extract_ids_from_text(
    text: &str,
    extracted_asset_references: &mut BTreeMap<String, ExtractedAssetReference>,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
) {
    let rbx_thumb_regex = get_rbx_thumb_regex();
    let rbx_asset_regex = get_rbx_asset_regex();
    let asset_url_regex = get_asset_url_regex();
    let raw_large_number_regex = get_raw_large_number_regex();
    let image_context_regex = get_image_context_regex();
    let mut explicit_ranges: Vec<(usize, usize)> = Vec::new();

    for thumb_match in rbx_thumb_regex.find_iter(text) {
        explicit_ranges.push((thumb_match.start(), thumb_match.end()));
        let raw_content = thumb_match.as_str().trim();
        if let Some(asset_id) = extract_rbxthumb_target_id(raw_content) {
            record_asset_reference(
                extracted_asset_references,
                asset_id,
                raw_content,
                instance_type,
                instance_name,
                instance_path,
                property_name,
            );
        }
    }

    for regex_match in rbx_asset_regex
        .captures_iter(text)
        .chain(asset_url_regex.captures_iter(text))
    {
        if let Some(full_match) = regex_match.get(0) {
            explicit_ranges.push((full_match.start(), full_match.end()));
        }
        if let Some(asset_id_match) = regex_match.get(1) {
            if let Ok(asset_id) = asset_id_match.as_str().trim().parse::<i64>() {
                record_asset_reference(
                    extracted_asset_references,
                    asset_id,
                    "",
                    instance_type,
                    instance_name,
                    instance_path,
                    property_name,
                );
            }
        }
    }

    for number_match in raw_large_number_regex.find_iter(text) {
        if range_overlaps_explicit(number_match.start(), number_match.end(), &explicit_ranges) {
            continue;
        }
        let context_start = number_match
            .start()
            .saturating_sub(RAW_ASSET_CONTEXT_WINDOW);
        let context_end = (number_match.end() + RAW_ASSET_CONTEXT_WINDOW).min(text.len());
        let context_bytes = &text.as_bytes()[context_start..context_end];
        let context_text = String::from_utf8_lossy(context_bytes);
        if !image_context_regex.is_match(&context_text) {
            continue;
        }
        if let Ok(asset_id) = number_match.as_str().trim().parse::<i64>() {
            record_asset_reference(
                extracted_asset_references,
                asset_id,
                "",
                instance_type,
                instance_name,
                instance_path,
                property_name,
            );
        }
    }
}

fn extract_ids_to_vec(
    text: &str,
    references: &mut Vec<ExtractedAssetReference>,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
) {
    let rbx_thumb_regex = get_rbx_thumb_regex();
    let rbx_asset_regex = get_rbx_asset_regex();
    let asset_url_regex = get_asset_url_regex();
    let raw_large_number_regex = get_raw_large_number_regex();
    let image_context_regex = get_image_context_regex();
    let mut explicit_ranges: Vec<(usize, usize)> = Vec::new();

    for thumb_match in rbx_thumb_regex.find_iter(text) {
        explicit_ranges.push((thumb_match.start(), thumb_match.end()));
        let raw_content = thumb_match.as_str().trim();
        if let Some(asset_id) = extract_rbxthumb_target_id(raw_content) {
            push_asset_reference(
                references,
                asset_id,
                raw_content,
                instance_type,
                instance_name,
                instance_path,
                property_name,
            );
        }
    }

    for regex_match in rbx_asset_regex
        .captures_iter(text)
        .chain(asset_url_regex.captures_iter(text))
    {
        if let Some(full_match) = regex_match.get(0) {
            explicit_ranges.push((full_match.start(), full_match.end()));
        }
        if let Some(asset_id_match) = regex_match.get(1) {
            if let Ok(asset_id) = asset_id_match.as_str().trim().parse::<i64>() {
                push_asset_reference(
                    references,
                    asset_id,
                    "",
                    instance_type,
                    instance_name,
                    instance_path,
                    property_name,
                );
            }
        }
    }

    for number_match in raw_large_number_regex.find_iter(text) {
        if range_overlaps_explicit(number_match.start(), number_match.end(), &explicit_ranges) {
            continue;
        }
        let context_start = number_match
            .start()
            .saturating_sub(RAW_ASSET_CONTEXT_WINDOW);
        let context_end = (number_match.end() + RAW_ASSET_CONTEXT_WINDOW).min(text.len());
        let context_bytes = &text.as_bytes()[context_start..context_end];
        let context_text = String::from_utf8_lossy(context_bytes);
        if !image_context_regex.is_match(&context_text) {
            continue;
        }
        if let Ok(asset_id) = number_match.as_str().trim().parse::<i64>() {
            push_asset_reference(
                references,
                asset_id,
                "",
                instance_type,
                instance_name,
                instance_path,
                property_name,
            );
        }
    }
}

fn record_asset_reference(
    extracted_asset_references: &mut BTreeMap<String, ExtractedAssetReference>,
    asset_id: i64,
    raw_content: &str,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
) {
    let reference_key = build_reference_key(asset_id, raw_content);
    let entry = extracted_asset_references
        .entry(reference_key)
        .or_insert_with(|| ExtractedAssetReference {
            id: asset_id,
            raw_content: String::new(),
            instance_type: String::new(),
            instance_name: String::new(),
            instance_path: String::new(),
            property_name: String::new(),
            used: 0,
            all_instance_paths: Vec::new(),
        });
    entry.used += 1;
    if entry.raw_content.is_empty() && !raw_content.trim().is_empty() {
        entry.raw_content = raw_content.trim().to_string();
    }
    if entry.instance_type.is_empty() && !instance_type.trim().is_empty() {
        entry.instance_type = instance_type.trim().to_string();
    }
    if entry.instance_name.is_empty() && !instance_name.trim().is_empty() {
        entry.instance_name = instance_name.trim().to_string();
    }
    let trimmed_path = instance_path.trim();
    if !trimmed_path.is_empty() {
        if entry.instance_path.is_empty() {
            entry.instance_path = trimmed_path.to_string();
        }
        if !entry.all_instance_paths.iter().any(|p| p == trimmed_path) {
            entry.all_instance_paths.push(trimmed_path.to_string());
        }
    }
    if entry.property_name.is_empty() && !property_name.trim().is_empty() {
        entry.property_name = property_name.trim().to_string();
    }
}

fn build_reference_key(asset_id: i64, raw_content: &str) -> String {
    let trimmed_raw_content = raw_content.trim();
    if !trimmed_raw_content.is_empty() {
        return trimmed_raw_content.to_ascii_lowercase();
    }
    asset_id.to_string()
}

fn push_asset_reference(
    references: &mut Vec<ExtractedAssetReference>,
    asset_id: i64,
    raw_content: &str,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
) {
    let trimmed_path = instance_path.trim().to_string();
    let paths = if trimmed_path.is_empty() {
        vec![]
    } else {
        vec![trimmed_path.clone()]
    };
    references.push(ExtractedAssetReference {
        id: asset_id,
        raw_content: raw_content.trim().to_string(),
        instance_type: instance_type.trim().to_string(),
        instance_name: instance_name.trim().to_string(),
        instance_path: trimmed_path,
        property_name: property_name.trim().to_string(),
        used: 1,
        all_instance_paths: paths,
    });
}

fn extract_positioned_ids_to_vec(
    text: &str,
    references: &mut Vec<PositionedAssetReference>,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
    world_position: Option<WorldPosition>,
) {
    let rbx_thumb_regex = get_rbx_thumb_regex();
    let rbx_asset_regex = get_rbx_asset_regex();
    let asset_url_regex = get_asset_url_regex();
    let raw_large_number_regex = get_raw_large_number_regex();
    let image_context_regex = get_image_context_regex();
    let mut explicit_ranges: Vec<(usize, usize)> = Vec::new();

    for thumb_match in rbx_thumb_regex.find_iter(text) {
        explicit_ranges.push((thumb_match.start(), thumb_match.end()));
        let raw_content = thumb_match.as_str().trim();
        if let Some(asset_id) = extract_rbxthumb_target_id(raw_content) {
            push_positioned_asset_reference(
                references,
                asset_id,
                raw_content,
                instance_type,
                instance_name,
                instance_path,
                property_name,
                world_position,
            );
        }
    }

    for regex_match in rbx_asset_regex
        .captures_iter(text)
        .chain(asset_url_regex.captures_iter(text))
    {
        if let Some(full_match) = regex_match.get(0) {
            explicit_ranges.push((full_match.start(), full_match.end()));
        }
        if let Some(asset_id_match) = regex_match.get(1) {
            if let Ok(asset_id) = asset_id_match.as_str().trim().parse::<i64>() {
                push_positioned_asset_reference(
                    references,
                    asset_id,
                    "",
                    instance_type,
                    instance_name,
                    instance_path,
                    property_name,
                    world_position,
                );
            }
        }
    }

    for number_match in raw_large_number_regex.find_iter(text) {
        if range_overlaps_explicit(number_match.start(), number_match.end(), &explicit_ranges) {
            continue;
        }
        let context_start = number_match
            .start()
            .saturating_sub(RAW_ASSET_CONTEXT_WINDOW);
        let context_end = (number_match.end() + RAW_ASSET_CONTEXT_WINDOW).min(text.len());
        let context_bytes = &text.as_bytes()[context_start..context_end];
        let context_text = String::from_utf8_lossy(context_bytes);
        if !image_context_regex.is_match(&context_text) {
            continue;
        }
        if let Ok(asset_id) = number_match.as_str().trim().parse::<i64>() {
            push_positioned_asset_reference(
                references,
                asset_id,
                "",
                instance_type,
                instance_name,
                instance_path,
                property_name,
                world_position,
            );
        }
    }
}

fn push_positioned_asset_reference(
    references: &mut Vec<PositionedAssetReference>,
    asset_id: i64,
    raw_content: &str,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
    world_position: Option<WorldPosition>,
) {
    references.push(PositionedAssetReference {
        id: asset_id,
        raw_content: raw_content.trim().to_string(),
        instance_type: instance_type.trim().to_string(),
        instance_name: instance_name.trim().to_string(),
        instance_path: instance_path.trim().to_string(),
        property_name: property_name.trim().to_string(),
        world_x: world_position.map(|value| value.x),
        world_y: world_position.map(|value| value.y),
        world_z: world_position.map(|value| value.z),
    });
}

fn collect_material_variant_bindings(
    dom: &WeakDom,
    path_prefixes: &[String],
    use_path_filter: bool,
) -> BTreeMap<String, Vec<MaterialVariantBinding>> {
    let mut bindings = BTreeMap::<String, Vec<MaterialVariantBinding>>::new();
    for instance in dom.descendants() {
        if normalize_for_match(instance.class.as_ref()) != "materialvariant" {
            continue;
        }
        let instance_path = build_instance_path(dom, instance);
        if use_path_filter && !instance_matches_path_prefixes(&instance_path, path_prefixes) {
            continue;
        }
        let variant_name_key = normalize_for_match(instance.name.as_ref());
        if variant_name_key.is_empty() {
            continue;
        }
        let references = collect_instance_asset_references(
            dom,
            instance,
            instance.class.as_ref(),
            instance.name.as_ref(),
            &instance_path,
        );
        if references.is_empty() {
            continue;
        }
        let base_material_key = find_property_value(instance, &["basematerial"])
            .and_then(variant_to_material_key)
            .unwrap_or_default();
        bindings
            .entry(variant_name_key)
            .or_default()
            .push(MaterialVariantBinding {
                base_material_key,
                references,
            });
    }
    bindings
}

fn resolve_material_variant_binding<'a>(
    instance: &Instance,
    bindings: &'a BTreeMap<String, Vec<MaterialVariantBinding>>,
) -> Option<&'a MaterialVariantBinding> {
    let material_variant_name =
        find_property_value(instance, &["materialvariant"]).and_then(variant_to_trimmed_string)?;
    let material_key =
        find_property_value(instance, &["material"]).and_then(variant_to_material_key);
    select_material_variant_binding(&material_variant_name, material_key.as_deref(), bindings)
}

fn select_material_variant_binding<'a>(
    material_variant_name: &str,
    material_key: Option<&str>,
    bindings: &'a BTreeMap<String, Vec<MaterialVariantBinding>>,
) -> Option<&'a MaterialVariantBinding> {
    let variant_name_key = normalize_for_match(material_variant_name);
    if variant_name_key.is_empty() {
        return None;
    }
    let candidates = bindings.get(&variant_name_key)?;
    if let Some(material_key) = material_key {
        if let Some(candidate) = candidates
            .iter()
            .find(|candidate| candidate.base_material_key == material_key)
        {
            return Some(candidate);
        }
    }
    if candidates.len() == 1 {
        return candidates.first();
    }
    None
}

fn collect_instance_asset_references(
    dom: &WeakDom,
    instance: &Instance,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
) -> Vec<ExtractedAssetReference> {
    let mut references = Vec::new();
    let meshpart_has_surface_color_map = instance_has_surface_color_map(dom, instance);
    for (property_name, property_value) in instance.properties.iter() {
        let normalized_property_name = normalize_for_match(property_name.as_ref());
        let rendered_property_value = format!("{:?}", property_value);
        if normalized_property_name.contains(PHYSICS_DATA_PROPERTY_TOKEN) {
            continue;
        }
        if meshpart_has_surface_color_map
            && is_meshpart_texture_property_name(&normalized_property_name)
        {
            continue;
        }
        if !is_asset_reference_property_name(&normalized_property_name)
            && !has_asset_reference(&rendered_property_value)
        {
            continue;
        }
        extract_ids_to_vec(
            &rendered_property_value,
            &mut references,
            instance_type,
            instance_name,
            instance_path,
            property_name.as_ref(),
        );
    }
    references
}

fn collect_effective_asset_references(
    dom: &WeakDom,
    instance: &Instance,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    bindings: &BTreeMap<String, Vec<MaterialVariantBinding>>,
) -> Vec<ExtractedAssetReference> {
    let mut references = Vec::new();
    if normalize_for_match(instance_type) != "materialvariant" {
        references.extend(collect_instance_asset_references(
            dom,
            instance,
            instance_type,
            instance_name,
            instance_path,
        ));
    }
    if let Some(binding) = resolve_material_variant_binding(instance, bindings) {
        append_material_variant_references(
            binding,
            &mut references,
            instance_type,
            instance_name,
            instance_path,
        );
    }
    references
}

fn collect_missing_material_variant_references(
    dom: &WeakDom,
    path_prefixes: &[String],
    use_path_filter: bool,
) -> Vec<MissingMaterialVariantReference> {
    let bindings = collect_material_variant_bindings(dom, path_prefixes, use_path_filter);
    let mut missing = Vec::<MissingMaterialVariantReference>::new();
    let mut seen_keys = BTreeMap::<String, bool>::new();

    for instance in dom.descendants() {
        let instance_path = build_instance_path(dom, instance);
        if use_path_filter && !instance_matches_path_prefixes(&instance_path, path_prefixes) {
            continue;
        }
        if normalize_for_match(instance.class.as_ref()) == "materialvariant" {
            continue;
        }

        let Some(material_variant_name) =
            find_property_value(instance, &["materialvariant"]).and_then(variant_to_trimmed_string)
        else {
            continue;
        };
        let material_key =
            find_property_value(instance, &["material"]).and_then(variant_to_material_key);
        if select_material_variant_binding(
            &material_variant_name,
            material_key.as_deref(),
            &bindings,
        )
        .is_some()
        {
            continue;
        }
        let variant_name_key = normalize_for_match(&material_variant_name);
        if bindings.contains_key(&variant_name_key) {
            continue;
        }

        let dedupe_key = format!(
            "{}|{}",
            variant_name_key,
            normalize_for_match(&instance_path)
        );
        if seen_keys.insert(dedupe_key, true).is_some() {
            continue;
        }
        missing.push(MissingMaterialVariantReference {
            variant_name: material_variant_name.trim().to_string(),
            instance_type: instance.class.trim().to_string(),
            instance_name: instance.name.trim().to_string(),
            instance_path: instance_path.trim().to_string(),
        });
    }

    missing
}

fn append_material_variant_references(
    binding: &MaterialVariantBinding,
    references: &mut Vec<ExtractedAssetReference>,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
) {
    for reference in &binding.references {
        let property_name = if reference.property_name.trim().is_empty() {
            "MaterialVariant".to_string()
        } else {
            format!("MaterialVariant.{}", reference.property_name.trim())
        };
        push_asset_reference(
            references,
            reference.id,
            &reference.raw_content,
            instance_type,
            instance_name,
            instance_path,
            &property_name,
        );
    }
}

fn append_positioned_instance_asset_references(
    dom: &WeakDom,
    instance: &Instance,
    references: &mut Vec<PositionedAssetReference>,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    world_position: Option<WorldPosition>,
) {
    let meshpart_has_surface_color_map = instance_has_surface_color_map(dom, instance);
    for (property_name, property_value) in instance.properties.iter() {
        let normalized_property_name = normalize_for_match(property_name.as_ref());
        let rendered_property_value = format!("{:?}", property_value);
        if normalized_property_name.contains(PHYSICS_DATA_PROPERTY_TOKEN) {
            continue;
        }
        if meshpart_has_surface_color_map
            && is_meshpart_texture_property_name(&normalized_property_name)
        {
            continue;
        }
        if !is_asset_reference_property_name(&normalized_property_name)
            && !has_asset_reference(&rendered_property_value)
        {
            continue;
        }
        extract_positioned_ids_to_vec(
            &rendered_property_value,
            references,
            instance_type,
            instance_name,
            instance_path,
            property_name.as_ref(),
            world_position,
        );
    }
}

fn append_positioned_material_variant_references(
    binding: &MaterialVariantBinding,
    references: &mut Vec<PositionedAssetReference>,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    world_position: Option<WorldPosition>,
) {
    for reference in &binding.references {
        let property_name = if reference.property_name.trim().is_empty() {
            "MaterialVariant".to_string()
        } else {
            format!("MaterialVariant.{}", reference.property_name.trim())
        };
        push_positioned_asset_reference(
            references,
            reference.id,
            &reference.raw_content,
            instance_type,
            instance_name,
            instance_path,
            &property_name,
            world_position,
        );
    }
}

fn build_instance_path(dom: &WeakDom, instance: &Instance) -> String {
    let mut path_segments: Vec<String> = Vec::new();
    let mut current_ref = instance.referent();
    loop {
        if current_ref == dom.root_ref() {
            break;
        }
        let Some(current_instance) = dom.get_by_ref(current_ref) else {
            break;
        };
        let trimmed_name = current_instance.name.trim();
        if !trimmed_name.is_empty() {
            path_segments.push(trimmed_name.to_string());
        }
        let parent_ref = current_instance.parent();
        if dom.get_by_ref(parent_ref).is_none() {
            break;
        }
        current_ref = parent_ref;
    }
    path_segments.reverse();
    path_segments.join(".")
}

fn resolve_instance_world_position(
    dom: &WeakDom,
    start_ref: rbx_dom_weak::types::Ref,
) -> Option<WorldPosition> {
    let mut current_ref = start_ref;
    loop {
        let current_instance = dom.get_by_ref(current_ref)?;
        if let Some(position) = extract_world_position_from_instance(current_instance) {
            return Some(position);
        }
        let parent_ref = current_instance.parent();
        if dom.get_by_ref(parent_ref).is_none() {
            return None;
        }
        current_ref = parent_ref;
    }
}

fn extract_world_position_from_instance(instance: &Instance) -> Option<WorldPosition> {
    if let Some(position_value) = find_property_value(instance, &["worldposition", "position"]) {
        if let Some(position) = variant_to_world_position(position_value) {
            return Some(position);
        }
    }
    if let Some(cframe_value) =
        find_property_value(instance, &["worldpivot", "worldcframe", "cframe"])
    {
        if let Some(position) = variant_to_world_position(cframe_value) {
            return Some(position);
        }
    }
    None
}

fn find_property_value<'a>(instance: &'a Instance, property_names: &[&str]) -> Option<&'a Variant> {
    for (property_name, property_value) in instance.properties.iter() {
        let normalized_property_name = normalize_for_match(property_name.as_ref());
        if property_names
            .iter()
            .any(|candidate| normalized_property_name == *candidate)
        {
            return Some(property_value);
        }
    }
    None
}

fn variant_to_trimmed_string(value: &Variant) -> Option<String> {
    match value {
        Variant::String(text) => non_empty_trimmed_string(text),
        Variant::BinaryString(text) => non_empty_trimmed_utf8(text.as_ref()),
        Variant::SharedString(text) => non_empty_trimmed_utf8(text.as_ref()),
        Variant::ContentId(content_id) => non_empty_trimmed_string(content_id.as_ref()),
        Variant::Content(content) => content.as_uri().and_then(non_empty_trimmed_string),
        _ => {
            let rendered_value = format!("{:?}", value);
            let first_quote = rendered_value.find('"')?;
            let remainder = &rendered_value[first_quote + 1..];
            let closing_quote = remainder.find('"')?;
            non_empty_trimmed_string(&remainder[..closing_quote])
        }
    }
}

fn variant_to_material_key(value: &Variant) -> Option<String> {
    let normalized = normalize_material_key(&format!("{:?}", value));
    if normalized.is_empty() {
        return None;
    }
    Some(normalized)
}

fn normalize_material_key(text: &str) -> String {
    text.chars()
        .filter(|character| character.is_ascii_alphanumeric())
        .collect::<String>()
        .to_ascii_lowercase()
}

fn non_empty_trimmed_string(text: &str) -> Option<String> {
    let trimmed = text.trim().trim_matches(char::from(0)).trim();
    if trimmed.is_empty() {
        return None;
    }
    Some(trimmed.to_string())
}

fn non_empty_trimmed_utf8(bytes: &[u8]) -> Option<String> {
    non_empty_trimmed_string(String::from_utf8_lossy(bytes).as_ref())
}

fn instance_has_surface_color_map(dom: &WeakDom, instance: &Instance) -> bool {
    normalize_for_match(instance.class.as_ref()) == "meshpart"
        && instance.children().iter().any(|child_ref| {
            if let Some(child_instance) = dom.get_by_ref(*child_ref) {
                if normalize_for_match(child_instance.class.as_ref()) != "surfaceappearance" {
                    return false;
                }
                for (child_property_name, child_property_value) in child_instance.properties.iter()
                {
                    if !normalize_for_match(child_property_name.as_ref()).contains("colormap") {
                        continue;
                    }
                    let rendered_color_map_value = format!("{:?}", child_property_value);
                    if has_non_empty_content_value(&rendered_color_map_value) {
                        return true;
                    }
                }
            }
            false
        })
}

fn variant_to_world_position(value: &Variant) -> Option<WorldPosition> {
    match value {
        Variant::Vector3(vector) => Some(WorldPosition {
            x: vector.x,
            y: vector.y,
            z: vector.z,
        }),
        Variant::CFrame(cframe) => Some(WorldPosition {
            x: cframe.position.x,
            y: cframe.position.y,
            z: cframe.position.z,
        }),
        Variant::OptionalCFrame(Some(cframe)) => Some(WorldPosition {
            x: cframe.position.x,
            y: cframe.position.y,
            z: cframe.position.z,
        }),
        _ => None,
    }
}

fn extract_map_render_part(instance: &Instance, instance_path: &str) -> Option<MapRenderPart> {
    if !is_supported_map_part_class(instance.class.as_ref()) {
        return None;
    }

    let cframe = variant_to_cframe(find_property_value(instance, &["cframe"])?)?;
    let size = variant_to_vector3(find_property_value(instance, &["size"])?)?;
    if size.x <= 0.0 || size.z <= 0.0 {
        return None;
    }
    let basis_size = find_property_value(instance, &["initialsize", "meshsize"])
        .and_then(variant_to_vector3)
        .unwrap_or(size);

    let (color_r, color_g, color_b) = find_property_value(instance, &["color"])
        .and_then(variant_to_color_rgb)
        .unwrap_or((163, 162, 165));
    let material_key = find_property_value(instance, &["material"])
        .and_then(variant_to_material_key)
        .unwrap_or_default();
    let transparency = find_property_value(instance, &["transparency"])
        .and_then(variant_to_f32)
        .map(|value| value.clamp(0.0, 1.0))
        .unwrap_or(0.0);
    let yaw_degrees = cframe
        .orientation
        .x
        .z
        .atan2(cframe.orientation.x.x)
        .to_degrees();

    Some(MapRenderPart {
        instance_type: instance.class.trim().to_string(),
        instance_name: instance.name.trim().to_string(),
        instance_path: instance_path.trim().to_string(),
        material_key,
        center_x: Some(cframe.position.x),
        center_y: Some(cframe.position.y),
        center_z: Some(cframe.position.z),
        size_x: Some(size.x),
        size_y: Some(size.y),
        size_z: Some(size.z),
        basis_size_x: Some(basis_size.x),
        basis_size_y: Some(basis_size.y),
        basis_size_z: Some(basis_size.z),
        yaw_degrees: Some(yaw_degrees),
        rotation_xx: Some(cframe.orientation.x.x),
        rotation_xy: Some(cframe.orientation.x.y),
        rotation_xz: Some(cframe.orientation.x.z),
        rotation_yx: Some(cframe.orientation.y.x),
        rotation_yy: Some(cframe.orientation.y.y),
        rotation_yz: Some(cframe.orientation.y.z),
        rotation_zx: Some(cframe.orientation.z.x),
        rotation_zy: Some(cframe.orientation.z.y),
        rotation_zz: Some(cframe.orientation.z.z),
        color_r: Some(color_r),
        color_g: Some(color_g),
        color_b: Some(color_b),
        transparency: Some(transparency),
    })
}

fn is_supported_map_part_class(instance_class: &str) -> bool {
    matches!(
        normalize_for_match(instance_class).as_str(),
        "part"
            | "meshpart"
            | "unionoperation"
            | "trusspart"
            | "wedgepart"
            | "cornerwedgepart"
            | "spawnlocation"
            | "seat"
            | "vehicleseat"
    )
}

fn variant_to_vector3(value: &Variant) -> Option<rbx_types::Vector3> {
    match value {
        Variant::Vector3(vector) => Some(*vector),
        Variant::Vector3int16(vector) => Some(rbx_types::Vector3 {
            x: vector.x as f32,
            y: vector.y as f32,
            z: vector.z as f32,
        }),
        _ => None,
    }
}

fn variant_to_cframe(value: &Variant) -> Option<rbx_types::CFrame> {
    match value {
        Variant::CFrame(cframe) => Some(*cframe),
        Variant::OptionalCFrame(Some(cframe)) => Some(*cframe),
        _ => None,
    }
}

fn variant_to_color_rgb(value: &Variant) -> Option<(u8, u8, u8)> {
    match value {
        Variant::Color3(color) => Some((
            (color.r.clamp(0.0, 1.0) * 255.0).round() as u8,
            (color.g.clamp(0.0, 1.0) * 255.0).round() as u8,
            (color.b.clamp(0.0, 1.0) * 255.0).round() as u8,
        )),
        Variant::Color3uint8(color) => Some((color.r, color.g, color.b)),
        _ => None,
    }
}

fn variant_to_f32(value: &Variant) -> Option<f32> {
    match value {
        Variant::Float32(number) => Some(*number),
        Variant::Float64(number) => Some(*number as f32),
        Variant::Int32(number) => Some(*number as f32),
        Variant::Int64(number) => Some(*number as f32),
        _ => None,
    }
}

fn range_overlaps_explicit(start: usize, end: usize, explicit_ranges: &[(usize, usize)]) -> bool {
    for (explicit_start, explicit_end) in explicit_ranges {
        if start < *explicit_end && *explicit_start < end {
            return true;
        }
    }
    false
}

fn is_asset_reference_property_name(normalized_property_name: &str) -> bool {
    for property_token in ASSET_REFERENCE_PROPERTY_TOKENS {
        if normalized_property_name.contains(property_token) {
            return true;
        }
    }
    false
}

fn normalize_for_match(text: &str) -> String {
    text.trim().to_ascii_lowercase()
}

fn get_rbx_asset_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| Regex::new(r"(?i)rbxassetid://\s*(\d+)").expect("valid regex"))
}

fn get_rbx_thumb_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| Regex::new(r#"(?i)rbxthumb://[^\s"'<>]+"#).expect("valid regex"))
}

fn get_asset_url_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| {
        Regex::new(r"(?i)https?://(?:www\.)?roblox\.com/asset/\?id=(\d+)").expect("valid regex")
    })
}

fn get_raw_large_number_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| Regex::new(r"\b\d{8,}\b").expect("valid regex"))
}

fn get_image_context_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| {
        Regex::new(
            r"(?i)(rbxassetid|assetid|texture|image|decal|thumbnail|shirt|pants|face|icon|content|mesh|sound|animation|font)",
        )
            .expect("valid regex")
    })
}

fn has_asset_reference(text: &str) -> bool {
    let rbx_thumb_regex = get_rbx_thumb_regex();
    let rbx_asset_regex = get_rbx_asset_regex();
    let asset_url_regex = get_asset_url_regex();
    let raw_large_number_regex = get_raw_large_number_regex();
    rbx_thumb_regex.is_match(text)
        || rbx_asset_regex.is_match(text)
        || asset_url_regex.is_match(text)
        || raw_large_number_regex.is_match(text)
}

fn extract_rbxthumb_target_id(raw_content: &str) -> Option<i64> {
    let normalized = raw_content.trim();
    let query = normalized.strip_prefix("rbxthumb://")?;
    for segment in query.split('&') {
        let (key, value) = segment.split_once('=')?;
        if key.trim().eq_ignore_ascii_case("id") || key.trim().eq_ignore_ascii_case("targetId") {
            if let Ok(parsed_id) = value.trim().parse::<i64>() {
                if parsed_id > 0 {
                    return Some(parsed_id);
                }
            }
        }
    }
    None
}

fn has_non_empty_content_value(text: &str) -> bool {
    let trimmed_text = text.trim();
    if trimmed_text.is_empty() {
        return false;
    }
    if trimmed_text.ends_with("(\"\")") || trimmed_text.ends_with("('')") {
        return false;
    }
    !trimmed_text.contains("Null")
}

fn is_meshpart_texture_property_name(normalized_property_name: &str) -> bool {
    normalized_property_name == "textureid" || normalized_property_name == "texturecontent"
}

fn run_replace(args: &[String]) -> Result<(), String> {
    if args.len() < 5 {
        return Err(
            "usage: joxblox-rusty-asset-tool replace <input.rbxl> <output.rbxl> <replacements.json>"
                .to_string(),
        );
    }
    let input_path = &args[2];
    let output_path = &args[3];
    let replacements_path = &args[4];

    let replacements_text = std::fs::read_to_string(replacements_path)
        .map_err(|e| format!("failed to read replacements: {}", e))?;
    let replacements: BTreeMap<String, i64> = serde_json::from_str(&replacements_text)
        .map_err(|e| format!("failed to parse replacements JSON: {}", e))?;

    if replacements.is_empty() {
        std::fs::copy(input_path, output_path)
            .map_err(|e| format!("failed to copy file: {}", e))?;
        println!("0");
        return Ok(());
    }

    let file = File::open(input_path).map_err(|e| format!("open failed: {}", e))?;
    let mut dom = from_reader(BufReader::new(file)).map_err(|e| format!("parse failed: {}", e))?;

    let referents: Vec<_> = dom.descendants().map(|inst| inst.referent()).collect();
    let mut total_replacements = 0usize;
    for referent in referents {
        if let Some(instance) = dom.get_by_ref_mut(referent) {
            for (_prop_name, prop_value) in instance.properties.iter_mut() {
                total_replacements += replace_ids_in_variant(prop_value, &replacements);
            }
        }
    }

    let output_file =
        File::create(output_path).map_err(|e| format!("create output failed: {}", e))?;
    rbx_binary::to_writer(BufWriter::new(output_file), &dom, dom.root().children())
        .map_err(|e| format!("serialize failed: {}", e))?;

    println!("{}", total_replacements);
    Ok(())
}

fn replace_ids_in_variant(variant: &mut Variant, replacements: &BTreeMap<String, i64>) -> usize {
    match variant {
        Variant::ContentId(content_id) => {
            let original = content_id.as_str().to_string();
            let replaced = replace_asset_ids_in_string(&original, replacements);
            if replaced != original {
                *content_id = ContentId::from(replaced.as_str());
                return 1;
            }
        }
        Variant::Content(content) => {
            if let Some(uri) = content.as_uri() {
                let original = uri.to_string();
                let replaced = replace_asset_ids_in_string(&original, replacements);
                if replaced != original {
                    *content = Content::from_uri(replaced);
                    return 1;
                }
            }
        }
        Variant::String(s) => {
            let replaced = replace_asset_ids_in_string(s, replacements);
            if replaced != *s {
                *s = replaced;
                return 1;
            }
        }
        _ => {}
    }
    0
}

fn get_replace_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| {
        Regex::new(r"(?i)(rbxassetid://\s*|https?://(?:www\.)?roblox\.com/asset/\?id=)(\d+)")
            .expect("valid regex")
    })
}

fn replace_asset_ids_in_string(text: &str, replacements: &BTreeMap<String, i64>) -> String {
    let re = get_replace_regex();
    let result = re.replace_all(text, |caps: &regex::Captures| {
        let prefix = caps.get(1).unwrap().as_str();
        let id_str = caps.get(2).unwrap().as_str();
        if let Some(new_id) = replacements.get(id_str) {
            format!("{}{}", prefix, new_id)
        } else {
            caps.get(0).unwrap().as_str().to_string()
        }
    });
    result.into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;
    use flate2::write::GzEncoder;
    use flate2::Compression;
    use rbx_dom_weak::InstanceBuilder;
    use rbx_types::BinaryString;
    use std::io::Write;

    #[test]
    fn detects_texturepack_xml_payload() {
        let xml = r#"
<roblox>
  <texturepack_version>2</texturepack_version>
  <url>rbxassetid://72886069858230</url>
</roblox>
"#;

        let decoded = decode_supported_text_payload(xml.as_bytes()).expect("expected xml payload");
        assert!(decoded.contains("<texturepack_version>2</texturepack_version>"));

        let references = extract_text_asset_references(&decoded, "texturepack.xml", None);
        assert_eq!(references.len(), 1);
        assert_eq!(references[0].id, 72886069858230);
    }

    #[test]
    fn detects_gzip_wrapped_texturepack_xml_payload() {
        let xml = r#"
<roblox>
  <texturepack_version>2</texturepack_version>
  <url>https://www.roblox.com/asset/?id=72886069858230</url>
</roblox>
"#;

        let mut encoder = GzEncoder::new(Vec::new(), Compression::default());
        encoder.write_all(xml.as_bytes()).expect("write gzip");
        let compressed = encoder.finish().expect("finish gzip");

        let decoded =
            decode_supported_text_payload(&compressed).expect("expected gzip xml payload");
        assert!(decoded.contains("72886069858230"));

        let references = extract_text_asset_references(&decoded, "texturepack.xml.gz", None);
        assert_eq!(references.len(), 1);
        assert_eq!(references[0].id, 72886069858230);
    }

    #[test]
    fn extract_text_asset_references_collapses_duplicate_ids() {
        let xml = r#"
<roblox>
  <texturepack_version>2</texturepack_version>
  <url>rbxassetid://72886069858230</url>
  <duplicate>rbxassetid://72886069858230</duplicate>
  <thumb>rbxthumb://type=Asset&id=72886069858230&w=150&h=150</thumb>
</roblox>
"#;

        let references = extract_text_asset_references(xml, "texturepack.xml", None);
        assert_eq!(references.len(), 2);
        assert_eq!(references[0].id, 72886069858230);
        assert_eq!(references[0].used, 2);
        assert_eq!(references[1].id, 72886069858230);
        assert_eq!(
            references[1].raw_content,
            "rbxthumb://type=Asset&id=72886069858230&w=150&h=150"
        );
    }

    #[test]
    fn extract_rbxthumb_target_id_supports_id_and_target_id() {
        assert_eq!(
            extract_rbxthumb_target_id("rbxthumb://type=Asset&id=72886069858230&w=150&h=150"),
            Some(72886069858230)
        );
        assert_eq!(
            extract_rbxthumb_target_id(
                "rbxthumb://type=Asset&targetId=72886069858230&size=150x150&format=Png"
            ),
            Some(72886069858230)
        );
        assert_eq!(
            extract_rbxthumb_target_id("rbxthumb://type=Asset&id=not-a-number&w=150&h=150"),
            None
        );
    }

    #[test]
    fn limit_triangle_indices_preserves_triangle_groups() {
        let indices = vec![0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11];
        let limited = limit_triangle_indices(&indices, Some(2));
        assert_eq!(limited, vec![0, 1, 2, 6, 7, 8]);
    }

    #[test]
    fn select_material_variant_binding_prefers_matching_base_material() {
        let mut bindings = BTreeMap::<String, Vec<MaterialVariantBinding>>::new();
        bindings.insert(
            "mud".to_string(),
            vec![
                MaterialVariantBinding {
                    base_material_key: "wood".to_string(),
                    references: vec![],
                },
                MaterialVariantBinding {
                    base_material_key: "ground".to_string(),
                    references: vec![],
                },
            ],
        );

        let selected = select_material_variant_binding("Mud", Some("ground"), &bindings)
            .expect("expected matching material variant");
        assert_eq!(selected.base_material_key, "ground");
    }

    #[test]
    fn select_material_variant_binding_falls_back_to_single_candidate() {
        let mut bindings = BTreeMap::<String, Vec<MaterialVariantBinding>>::new();
        bindings.insert(
            "mud".to_string(),
            vec![MaterialVariantBinding {
                base_material_key: "ground".to_string(),
                references: vec![],
            }],
        );

        let selected = select_material_variant_binding("Mud", Some("wood"), &bindings)
            .expect("expected single material variant fallback");
        assert_eq!(selected.base_material_key, "ground");
    }

    #[test]
    fn variant_to_trimmed_string_supports_binary_string_values() {
        let value = Variant::BinaryString(BinaryString::from(b"AlcantaraHD\0".to_vec()));
        assert_eq!(
            variant_to_trimmed_string(&value).as_deref(),
            Some("AlcantaraHD")
        );
    }

    #[test]
    fn collect_effective_asset_references_expands_material_variant_bindings() {
        let meshpart = InstanceBuilder::new("MeshPart")
            .with_property("MaterialVariant", Variant::String("Mud".to_string()))
            .with_property("Material", Variant::String("Ground".to_string()));
        let meshpart_ref = meshpart.referent();
        let dom = WeakDom::new(InstanceBuilder::new("DataModel").with_child(meshpart));
        let instance = dom
            .get_by_ref(meshpart_ref)
            .expect("expected test meshpart to exist");

        let mut bindings = BTreeMap::<String, Vec<MaterialVariantBinding>>::new();
        bindings.insert(
            "mud".to_string(),
            vec![MaterialVariantBinding {
                base_material_key: "ground".to_string(),
                references: vec![ExtractedAssetReference {
                    id: 123,
                    raw_content: "rbxassetid://123".to_string(),
                    instance_type: "MaterialVariant".to_string(),
                    instance_name: "Mud".to_string(),
                    instance_path: "MaterialService.Mud".to_string(),
                    property_name: "ColorMapContent".to_string(),
                    used: 1,
                    all_instance_paths: vec!["MaterialService.Mud".to_string()],
                }],
            }],
        );

        let references = collect_effective_asset_references(
            &dom,
            instance,
            instance.class.as_ref(),
            instance.name.as_ref(),
            "MeshPart",
            &bindings,
        );

        assert_eq!(references.len(), 1);
        assert_eq!(references[0].id, 123);
        assert_eq!(references[0].instance_path, "MeshPart");
        assert_eq!(
            references[0].property_name,
            "MaterialVariant.ColorMapContent"
        );
    }

    fn build_v4_mesh(
        num_verts: u32,
        num_faces: u32,
        num_bones: u16,
        lod_offsets: &[u32],
    ) -> Vec<u8> {
        let mut data = Vec::new();
        data.extend_from_slice(b"version 4.01\n");
        let num_lod_offsets = lod_offsets.len() as u16;
        let sizeof_header: u16 = 24;
        data.extend_from_slice(&sizeof_header.to_le_bytes());
        data.extend_from_slice(&0u16.to_le_bytes()); // lodType
        data.extend_from_slice(&num_verts.to_le_bytes());
        data.extend_from_slice(&num_faces.to_le_bytes());
        data.extend_from_slice(&num_lod_offsets.to_le_bytes());
        data.extend_from_slice(&num_bones.to_le_bytes());
        data.extend_from_slice(&0u32.to_le_bytes()); // padding to sizeof_header=24
        data.extend_from_slice(&0u32.to_le_bytes());
        // vertex data: 40 bytes each (pos xyz = 12 bytes + 28 bytes padding)
        for i in 0..num_verts {
            let x = (i as f32) * 0.5;
            let y = (i as f32) * 1.0;
            let z = (i as f32) * -0.5;
            data.extend_from_slice(&x.to_le_bytes());
            data.extend_from_slice(&y.to_le_bytes());
            data.extend_from_slice(&z.to_le_bytes());
            data.extend_from_slice(&[0u8; 28]); // normal+uv+tangent+color
        }
        // skinning data
        if num_bones > 0 {
            for _ in 0..num_verts {
                data.extend_from_slice(&[0u8; 8]);
            }
        }
        // face data: 12 bytes each (3 x u32)
        for i in 0..num_faces {
            let base = i * 3;
            data.extend_from_slice(&base.to_le_bytes());
            data.extend_from_slice(&(base + 1).to_le_bytes());
            data.extend_from_slice(&(base + 2).to_le_bytes());
        }
        // LOD offsets
        for offset in lod_offsets {
            data.extend_from_slice(&offset.to_le_bytes());
        }
        data
    }

    #[test]
    fn parse_v4_mesh_preview_extracts_positions_and_indices() {
        let data = build_v4_mesh(3, 1, 0, &[]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.format_version, "4.01");
        assert_eq!(result.decoder_source, "binary");
        assert_eq!(result.vertex_count, 3);
        assert_eq!(result.triangle_count, 1);
        assert_eq!(result.positions.len(), 9);
        assert_eq!(result.indices, vec![0, 1, 2]);
        assert!((result.positions[0] - 0.0).abs() < 1e-6);
        assert!((result.positions[3] - 0.5).abs() < 1e-6);
        assert!((result.positions[6] - 1.0).abs() < 1e-6);
    }

    #[test]
    fn parse_v4_mesh_preview_reports_all_lods() {
        // LOD table [0, 2, 3] means two LODs: LOD 0 covers faces [0..2) (2
        // triangles) and LOD 1 covers faces [2..3) (1 triangle). The preview
        // now returns the full geometry (all 3 triangles) plus per-LOD
        // triangle ranges so the UI can render each LOD independently.
        let data = build_v4_mesh(9, 3, 0, &[0, 2, 3]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.triangle_count, 3);
        assert_eq!(result.indices.len(), 9);
        assert_eq!(result.lods.len(), 2);
        assert_eq!(result.lods[0].triangle_start, 0);
        assert_eq!(result.lods[0].triangle_end, 2);
        assert_eq!(result.lods[1].triangle_start, 2);
        assert_eq!(result.lods[1].triangle_end, 3);
    }

    #[test]
    fn parse_v4_mesh_preview_single_lod_fallback_when_no_table() {
        // Empty LOD table -> the parser synthesises a single LOD covering
        // every triangle so downstream UIs always see at least one entry.
        let data = build_v4_mesh(3, 1, 0, &[]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.lods.len(), 1);
        assert_eq!(result.lods[0].triangle_start, 0);
        assert_eq!(result.lods[0].triangle_end, 1);
    }

    #[test]
    fn parse_v4_mesh_stats_still_reports_lod0_count() {
        // Regression: `parse_mesh_stats` must continue to report only the
        // LOD 0 face count so that asset reports / heatmap totals match
        // what Roblox would actually render at full detail.
        let data = build_v4_mesh(9, 3, 0, &[0, 2, 3]);
        let result = parse_mesh_stats(&data).expect("parse_mesh_stats failed");
        assert_eq!(result.triangle_count, 2);
    }

    #[test]
    fn parse_v4_mesh_stats_returns_counts() {
        let data = build_v4_mesh(6, 2, 0, &[]);
        let result = parse_mesh_stats(&data).expect("parse_mesh_stats failed");
        assert_eq!(result.vertex_count, 6);
        assert_eq!(result.triangle_count, 2);
    }

    #[test]
    fn parse_v4_mesh_with_bones() {
        let data = build_v4_mesh(3, 1, 2, &[]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.vertex_count, 3);
        assert_eq!(result.triangle_count, 1);
        assert_eq!(result.indices, vec![0, 1, 2]);
    }

    /// Build a synthetic v3 mesh with an explicit vertex/face stride so we
    /// exercise the "variable stride" path. When `lod_offsets` is empty no
    /// LOD table is appended.
    fn build_v3_mesh(
        version: &str,
        num_verts: u32,
        num_faces: u32,
        sizeof_vertex: u8,
        sizeof_face: u8,
        lod_offsets: &[u32],
    ) -> Vec<u8> {
        assert!(sizeof_vertex as usize >= 12, "vertex stride must fit XYZ");
        assert!(sizeof_face as usize >= 12, "face stride must fit index triplet");
        let mut data = Vec::new();
        data.extend_from_slice(format!("version {}\n", version).as_bytes());
        let sizeof_header: u16 = 16;
        let num_lod_offsets = lod_offsets.len() as u16;
        data.extend_from_slice(&sizeof_header.to_le_bytes());
        data.push(sizeof_vertex);
        data.push(sizeof_face);
        data.extend_from_slice(&0u16.to_le_bytes()); // lodType / padding at +4
        data.extend_from_slice(&num_lod_offsets.to_le_bytes()); // +6
        data.extend_from_slice(&num_verts.to_le_bytes()); // +8
        data.extend_from_slice(&num_faces.to_le_bytes()); // +12
        for i in 0..num_verts {
            let x = (i as f32) * 0.25;
            let y = (i as f32) * 0.5;
            let z = (i as f32) * -0.25;
            data.extend_from_slice(&x.to_le_bytes());
            data.extend_from_slice(&y.to_le_bytes());
            data.extend_from_slice(&z.to_le_bytes());
            let padding = sizeof_vertex as usize - 12;
            data.extend(std::iter::repeat(0u8).take(padding));
        }
        for i in 0..num_faces {
            let base = i * 3;
            data.extend_from_slice(&base.to_le_bytes());
            data.extend_from_slice(&(base + 1).to_le_bytes());
            data.extend_from_slice(&(base + 2).to_le_bytes());
            let padding = sizeof_face as usize - 12;
            data.extend(std::iter::repeat(0u8).take(padding));
        }
        for offset in lod_offsets {
            data.extend_from_slice(&offset.to_le_bytes());
        }
        data
    }

    #[test]
    fn parse_v3_mesh_preview_extracts_positions_and_indices() {
        let data = build_v3_mesh("3.01", 3, 1, 36, 12, &[]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.format_version, "3.01");
        assert_eq!(result.decoder_source, "binary");
        assert_eq!(result.vertex_count, 3);
        assert_eq!(result.triangle_count, 1);
        assert_eq!(result.positions.len(), 9);
        assert_eq!(result.indices, vec![0, 1, 2]);
        assert!((result.positions[0] - 0.0).abs() < 1e-6);
        assert!((result.positions[3] - 0.25).abs() < 1e-6);
        assert!((result.positions[6] - 0.5).abs() < 1e-6);
        assert_eq!(result.lods.len(), 1);
        assert_eq!(result.lods[0].triangle_start, 0);
        assert_eq!(result.lods[0].triangle_end, 1);
    }

    #[test]
    fn parse_v3_mesh_preview_reports_all_lods() {
        // LOD table [0, 2, 3] -> two LODs (LOD 0 faces [0..2), LOD 1 face
        // [2..3)). The preview returns the full geometry plus per-LOD
        // ranges so the LOD viewer can render each LOD independently.
        let data = build_v3_mesh("3.00", 9, 3, 36, 12, &[0, 2, 3]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.format_version, "3.00");
        assert_eq!(result.triangle_count, 3);
        assert_eq!(result.indices.len(), 9);
        assert_eq!(result.lods.len(), 2);
        assert_eq!(result.lods[0].triangle_start, 0);
        assert_eq!(result.lods[0].triangle_end, 2);
        assert_eq!(result.lods[1].triangle_start, 2);
        assert_eq!(result.lods[1].triangle_end, 3);
    }

    #[test]
    fn parse_v3_mesh_preview_handles_40byte_vertex_stride() {
        // Older v3 exports ship with a 40-byte vertex (includes per-vertex
        // colour). The parser must read positions from the first 12 bytes
        // and skip the remaining 28.
        let data = build_v3_mesh("3.00", 3, 1, 40, 12, &[]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.vertex_count, 3);
        assert_eq!(result.indices, vec![0, 1, 2]);
        assert!((result.positions[3] - 0.25).abs() < 1e-6);
    }

    #[test]
    fn parse_v3_mesh_stats_reports_lod0_count() {
        // parse_mesh_stats should report the LOD 0 face count (2) not the
        // total face count (3) for consistency with v4 stats behaviour.
        let data = build_v3_mesh("3.01", 9, 3, 36, 12, &[0, 2, 3]);
        let result = parse_mesh_stats(&data).expect("parse_mesh_stats failed");
        assert_eq!(result.format_version, "3.01");
        assert_eq!(result.vertex_count, 9);
        assert_eq!(result.triangle_count, 2);
    }

    #[test]
    fn parse_v1_mesh_preview_parses_ascii_triangles() {
        let data = b"version 1.00\n2\n[0,0,0][0,1,0][0,0]\n[1,0,0][0,1,0][1,0]\n[0,1,0][0,1,0][0,1]\n[1,1,1][0,1,0][1,1]\n[2,1,1][0,1,0][1,0]\n[1,2,1][0,1,0][0,1]\n";
        let result = parse_mesh_preview(data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.format_version, "1.00");
        assert_eq!(result.decoder_source, "text");
        assert_eq!(result.triangle_count, 2);
        assert_eq!(result.vertex_count, 6);
        assert_eq!(result.indices, (0..6).collect::<Vec<u32>>());
        assert_eq!(result.positions.len(), 18);
        assert!((result.positions[3] - 1.0).abs() < 1e-6); // 2nd vertex x
        assert!((result.positions[9] - 1.0).abs() < 1e-6); // 4th vertex x (triangle 2 start)
        assert_eq!(result.lods.len(), 1);
        assert_eq!(result.lods[0].triangle_end, 2);
    }

    #[test]
    fn parse_v1_mesh_preview_rejects_truncated_face_list() {
        let data = b"version 1.01\n2\n[0,0,0][0,1,0][0,0]\n[1,0,0][0,1,0][1,0]\n[0,1,0][0,1,0][0,1]\n";
        match parse_mesh_preview(data, None) {
            Ok(_) => panic!("expected truncated mesh error, got Ok"),
            Err(err) => assert!(err.contains("expected 6"), "error was: {}", err),
        }
    }

    #[test]
    fn parse_v1_mesh_stats_reports_counts() {
        let data = b"version 1.00\n1\n[0,0,0][0,1,0][0,0]\n[1,0,0][0,1,0][1,0]\n[0,1,0][0,1,0][0,1]\n";
        let result = parse_mesh_stats(data).expect("parse_mesh_stats failed");
        assert_eq!(result.format_version, "1.00");
        assert_eq!(result.decoder_source, "text");
        assert_eq!(result.vertex_count, 3);
        assert_eq!(result.triangle_count, 1);
    }

    /// Build a synthetic v2 mesh with an explicit vertex/face stride. v2
    /// never ships an LOD offset table.
    fn build_v2_mesh(num_verts: u32, num_faces: u32, sizeof_vertex: u8, sizeof_face: u8) -> Vec<u8> {
        assert!(sizeof_vertex as usize >= 12);
        assert!(sizeof_face as usize >= 12);
        let mut data = Vec::new();
        data.extend_from_slice(b"version 2.00\n");
        let sizeof_header: u16 = 12;
        data.extend_from_slice(&sizeof_header.to_le_bytes());
        data.push(sizeof_vertex);
        data.push(sizeof_face);
        data.extend_from_slice(&num_verts.to_le_bytes());
        data.extend_from_slice(&num_faces.to_le_bytes());
        for i in 0..num_verts {
            let x = (i as f32) * 0.125;
            let y = (i as f32) * 0.25;
            let z = (i as f32) * -0.125;
            data.extend_from_slice(&x.to_le_bytes());
            data.extend_from_slice(&y.to_le_bytes());
            data.extend_from_slice(&z.to_le_bytes());
            data.extend(std::iter::repeat(0u8).take(sizeof_vertex as usize - 12));
        }
        for i in 0..num_faces {
            let base = i * 3;
            data.extend_from_slice(&base.to_le_bytes());
            data.extend_from_slice(&(base + 1).to_le_bytes());
            data.extend_from_slice(&(base + 2).to_le_bytes());
            data.extend(std::iter::repeat(0u8).take(sizeof_face as usize - 12));
        }
        data
    }

    #[test]
    fn parse_v2_mesh_preview_extracts_geometry() {
        let data = build_v2_mesh(3, 1, 36, 12);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.format_version, "2.00");
        assert_eq!(result.decoder_source, "binary");
        assert_eq!(result.vertex_count, 3);
        assert_eq!(result.triangle_count, 1);
        assert_eq!(result.indices, vec![0, 1, 2]);
        assert_eq!(result.positions.len(), 9);
        assert!((result.positions[3] - 0.125).abs() < 1e-6);
        assert_eq!(result.lods.len(), 1);
    }

    #[test]
    fn parse_v2_mesh_stats_still_works() {
        let data = build_v2_mesh(6, 2, 40, 12);
        let result = parse_mesh_stats(&data).expect("parse_mesh_stats failed");
        assert_eq!(result.vertex_count, 6);
        assert_eq!(result.triangle_count, 2);
    }

    /// Build a synthetic v6.00 mesh. The header layout is identical to v4/v5
    /// (40-byte vertex stride, 12-byte face stride), so we reuse
    /// `build_v4_mesh` with a patched version string.
    fn build_v6_mesh(num_verts: u32, num_faces: u32, lod_offsets: &[u32]) -> Vec<u8> {
        let mut data = build_v4_mesh(num_verts, num_faces, 0, lod_offsets);
        // Overwrite the "version 4.01\n" prefix with "version 6.00\n" (both
        // are exactly 13 bytes).
        let prefix = b"version 6.00\n";
        assert_eq!(prefix.len(), 13);
        data[..prefix.len()].copy_from_slice(prefix);
        data
    }

    #[test]
    fn parse_v6_mesh_preview_uses_v4_layout() {
        let data = build_v6_mesh(3, 1, &[]);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.format_version, "6.00");
        assert_eq!(result.triangle_count, 1);
        assert_eq!(result.indices, vec![0, 1, 2]);
        assert_eq!(result.lods.len(), 1);
    }

    /// Build a synthetic v7.00 uncompressed CoreMesh body (no Draco payload).
    ///
    /// Layout (relative to body start, i.e. after `version 7.00\n`):
    ///   +0..+7   "COREMESH"
    ///   +8..+15  8 bytes of body-header padding
    ///   +16..+19 u32 num_verts
    ///   +20..    num_verts × 40-byte vertices, then u32 num_faces, then
    ///            num_faces × 12-byte indices.
    fn build_v7_uncompressed_mesh(num_verts: u32, num_faces: u32) -> Vec<u8> {
        let mut data = Vec::new();
        data.extend_from_slice(b"version 7.00\n");
        data.extend_from_slice(b"COREMESH");
        data.extend_from_slice(&[0u8; 8]);
        data.extend_from_slice(&num_verts.to_le_bytes());
        for i in 0..num_verts {
            let x = (i as f32) * 0.5;
            let y = (i as f32) * 0.25;
            let z = (i as f32) * -0.5;
            data.extend_from_slice(&x.to_le_bytes());
            data.extend_from_slice(&y.to_le_bytes());
            data.extend_from_slice(&z.to_le_bytes());
            data.extend_from_slice(&[0u8; 28]);
        }
        data.extend_from_slice(&num_faces.to_le_bytes());
        for i in 0..num_faces {
            let base = i * 3;
            data.extend_from_slice(&base.to_le_bytes());
            data.extend_from_slice(&(base + 1).to_le_bytes());
            data.extend_from_slice(&(base + 2).to_le_bytes());
        }
        data
    }

    #[test]
    fn parse_v7_uncompressed_mesh_preview_extracts_geometry() {
        let data = build_v7_uncompressed_mesh(3, 1);
        let result = parse_mesh_preview(&data, None).expect("parse_mesh_preview failed");
        assert_eq!(result.format_version, "7.00");
        assert_eq!(result.decoder_source, "binary");
        assert_eq!(result.vertex_count, 3);
        assert_eq!(result.triangle_count, 1);
        assert_eq!(result.indices, vec![0, 1, 2]);
        assert_eq!(result.lods.len(), 1);
    }

    #[test]
    fn parse_v7_uncompressed_mesh_stats_reports_counts() {
        let data = build_v7_uncompressed_mesh(6, 2);
        let result = parse_mesh_stats(&data).expect("parse_mesh_stats failed");
        assert_eq!(result.format_version, "7.00");
        assert_eq!(result.decoder_source, "binary");
        assert_eq!(result.vertex_count, 6);
        assert_eq!(result.triangle_count, 2);
    }
}
