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

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MapRenderPart {
    instance_type: String,
    instance_name: String,
    instance_path: String,
    center_x: Option<f32>,
    center_y: Option<f32>,
    center_z: Option<f32>,
    size_x: Option<f32>,
    size_y: Option<f32>,
    size_z: Option<f32>,
    yaw_degrees: Option<f32>,
    color_r: Option<u8>,
    color_g: Option<u8>,
    color_b: Option<u8>,
    transparency: Option<f32>,
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
    for instance in dom.descendants() {
        let instance_type = instance.class.as_ref();
        let instance_name = instance.name.as_ref();
        let instance_path = build_instance_path(&dom, instance);
        let meshpart_has_surface_color_map = normalize_for_match(instance.class.as_ref())
            == "meshpart"
            && instance.children().iter().any(|child_ref| {
                if let Some(child_instance) = dom.get_by_ref(*child_ref) {
                    if normalize_for_match(child_instance.class.as_ref()) != "surfaceappearance" {
                        return false;
                    }
                    for (child_property_name, child_property_value) in
                        child_instance.properties.iter()
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
            });
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
            extract_ids_from_text(
                &rendered_property_value,
                &mut extracted_asset_references,
                instance_type,
                instance_name,
                &instance_path,
                property_name.as_ref(),
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

fn parse_mesh_stats(data: &[u8]) -> Result<MeshStatsResult, String> {
    if data.len() < 13 {
        return Err("mesh data too short".to_string());
    }

    let version_header =
        std::str::from_utf8(&data[..13]).map_err(|e| format!("invalid mesh header: {}", e))?;
    if !version_header.starts_with("version ") {
        return Err("not a Roblox mesh file".to_string());
    }
    let version = version_header["version ".len()..].trim().to_string();
    let body = &data[13..];

    if version == "7.00" && body.starts_with(b"COREMESH") {
        let draco_start =
            find_draco_payload_start(body).ok_or_else(|| "missing Draco payload".to_string())?;
        let draco_payload = &body[draco_start..];
        let decode_result = decode_mesh_with_config_sync(draco_payload)
            .ok_or_else(|| "Draco decode failed".to_string())?;
        let vertex_count = decode_result.config.vertex_count() as u32;
        let triangle_count = (decode_result.config.index_count() / 3) as u32;
        return Ok(MeshStatsResult {
            format_version: version,
            decoder_source: "draco".to_string(),
            vertex_count,
            triangle_count,
        });
    }

    Err(format!(
        "unsupported mesh stats format: version {}",
        version
    ))
}

fn parse_mesh_preview(
    data: &[u8],
    max_triangles: Option<usize>,
) -> Result<MeshPreviewResult, String> {
    if data.len() < 13 {
        return Err("mesh data too short".to_string());
    }

    let version_header =
        std::str::from_utf8(&data[..13]).map_err(|e| format!("invalid mesh header: {}", e))?;
    if !version_header.starts_with("version ") {
        return Err("not a Roblox mesh file".to_string());
    }
    let version = version_header["version ".len()..].trim().to_string();
    let body = &data[13..];

    if version == "7.00" && body.starts_with(b"COREMESH") {
        let draco_start =
            find_draco_payload_start(body).ok_or_else(|| "missing Draco payload".to_string())?;
        let draco_payload = &body[draco_start..];
        let decode_result = decode_mesh_with_config_sync(draco_payload)
            .ok_or_else(|| "Draco decode failed".to_string())?;
        let positions = extract_position_components(&decode_result)?;
        let all_indices = extract_triangle_indices(&decode_result)?;
        let preview_indices = limit_triangle_indices(&all_indices, max_triangles);
        return Ok(MeshPreviewResult {
            format_version: version,
            decoder_source: "draco".to_string(),
            vertex_count: decode_result.config.vertex_count(),
            triangle_count: (all_indices.len() / 3) as u32,
            preview_triangle_count: (preview_indices.len() / 3) as u32,
            positions,
            indices: preview_indices,
        });
    }

    Err(format!(
        "unsupported mesh preview format: version {}",
        version
    ))
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

    for instance in dom.descendants() {
        let instance_path = build_instance_path(&dom, instance);
        if use_path_filter && !instance_matches_path_prefixes(&instance_path, &path_prefixes) {
            continue;
        }

        let instance_type = instance.class.as_ref();
        let instance_name = instance.name.as_ref();
        let world_position = resolve_instance_world_position(&dom, instance.referent());
        let meshpart_has_surface_color_map = normalize_for_match(instance.class.as_ref())
            == "meshpart"
            && instance.children().iter().any(|child_ref| {
                if let Some(child_instance) = dom.get_by_ref(*child_ref) {
                    if normalize_for_match(child_instance.class.as_ref()) != "surfaceappearance" {
                        return false;
                    }
                    for (child_property_name, child_property_value) in
                        child_instance.properties.iter()
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
            });

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
                &mut references,
                instance_type,
                instance_name,
                &instance_path,
                property_name.as_ref(),
                world_position,
            );
        }
    }

    let output = serde_json::to_string(&references)
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

    for instance in dom.descendants() {
        let instance_path = build_instance_path(&dom, instance);
        if !instance_matches_path_prefixes(&instance_path, path_prefixes) {
            continue;
        }

        let instance_type = instance.class.as_ref();
        let instance_name = instance.name.as_ref();
        let meshpart_has_surface_color_map = normalize_for_match(instance.class.as_ref())
            == "meshpart"
            && instance.children().iter().any(|child_ref| {
                if let Some(child_instance) = dom.get_by_ref(*child_ref) {
                    if normalize_for_match(child_instance.class.as_ref()) != "surfaceappearance" {
                        return false;
                    }
                    for (child_property_name, child_property_value) in
                        child_instance.properties.iter()
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
            });

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
                &mut all_references,
                instance_type,
                instance_name,
                &instance_path,
                property_name.as_ref(),
            );
        }
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

    let (color_r, color_g, color_b) = find_property_value(instance, &["color"])
        .and_then(variant_to_color_rgb)
        .unwrap_or((163, 162, 165));
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
        center_x: Some(cframe.position.x),
        center_y: Some(cframe.position.y),
        center_z: Some(cframe.position.z),
        size_x: Some(size.x),
        size_y: Some(size.y),
        size_z: Some(size.z),
        yaw_degrees: Some(yaw_degrees),
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
}
