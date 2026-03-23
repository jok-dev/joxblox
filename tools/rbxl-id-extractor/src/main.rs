use std::collections::BTreeMap;
use std::env;
use std::fs::File;
use std::io::BufReader;
use std::sync::OnceLock;

use rbx_binary::from_reader;
use rbx_dom_weak::{Instance, WeakDom};
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
    instance_type: String,
    instance_name: String,
    instance_path: String,
    property_name: String,
    used: usize,
}

fn main() {
    if let Err(err) = run() {
        eprintln!("{}", err);
        std::process::exit(1);
    }
}

fn run() -> Result<(), String> {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        return Err("usage: rbxl-id-extractor <rbxl-file> [max-results]".to_string());
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

    let file_bytes =
        std::fs::read(file_path).map_err(|read_err| format!("read failed: {}", read_err))?;
    let mut extracted_asset_references = BTreeMap::<i64, ExtractedAssetReference>::new();
    let file = File::open(file_path).map_err(|open_err| format!("open failed: {}", open_err))?;
    if let Ok(dom) = from_reader(BufReader::new(file)) {
        for instance in dom.descendants() {
            let instance_type = instance.class.as_ref();
            let instance_name = instance.name.as_ref();
            let instance_path = build_instance_path(&dom, instance);
            let meshpart_has_surface_color_map = normalize_for_match(instance.class.as_ref())
                == "meshpart"
                && instance.children().iter().any(|child_ref| {
                    if let Some(child_instance) = dom.get_by_ref(*child_ref) {
                        if normalize_for_match(child_instance.class.as_ref()) != "surfaceappearance"
                        {
                            return false;
                        }
                        for (child_property_name, child_property_value) in
                            child_instance.properties.iter()
                        {
                            if !normalize_for_match(child_property_name.as_ref())
                                .contains("colormap")
                            {
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
        extract_ids_from_text(
            &String::from_utf8_lossy(&file_bytes),
            &mut extracted_asset_references,
            "",
            "",
            "",
            "",
        );
    } else {
        extract_ids_from_text(
            &String::from_utf8_lossy(&file_bytes),
            &mut extracted_asset_references,
            "",
            "",
            "",
            "",
        );
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

fn extract_ids_from_text(
    text: &str,
    extracted_asset_references: &mut BTreeMap<i64, ExtractedAssetReference>,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
) {
    let rbx_asset_regex = get_rbx_asset_regex();
    let asset_url_regex = get_asset_url_regex();
    let raw_large_number_regex = get_raw_large_number_regex();
    let image_context_regex = get_image_context_regex();
    let mut explicit_ranges: Vec<(usize, usize)> = Vec::new();

    for regex_match in rbx_asset_regex
        .captures_iter(text)
        .chain(asset_url_regex.captures_iter(text))
    {
        if let Some(asset_id_match) = regex_match.get(1) {
            explicit_ranges.push((asset_id_match.start(), asset_id_match.end()));
            if let Ok(asset_id) = asset_id_match.as_str().trim().parse::<i64>() {
                record_asset_reference(
                    extracted_asset_references,
                    asset_id,
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
                instance_type,
                instance_name,
                instance_path,
                property_name,
            );
        }
    }
}

fn record_asset_reference(
    extracted_asset_references: &mut BTreeMap<i64, ExtractedAssetReference>,
    asset_id: i64,
    instance_type: &str,
    instance_name: &str,
    instance_path: &str,
    property_name: &str,
) {
    let entry = extracted_asset_references
        .entry(asset_id)
        .or_insert_with(|| ExtractedAssetReference {
            id: asset_id,
            instance_type: String::new(),
            instance_name: String::new(),
            instance_path: String::new(),
            property_name: String::new(),
            used: 0,
        });
    entry.used += 1;
    if entry.instance_type.is_empty() && !instance_type.trim().is_empty() {
        entry.instance_type = instance_type.trim().to_string();
    }
    if entry.instance_name.is_empty() && !instance_name.trim().is_empty() {
        entry.instance_name = instance_name.trim().to_string();
    }
    if entry.instance_path.is_empty() && !instance_path.trim().is_empty() {
        entry.instance_path = instance_path.trim().to_string();
    }
    if entry.property_name.is_empty() && !property_name.trim().is_empty() {
        entry.property_name = property_name.trim().to_string();
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
    let rbx_asset_regex = get_rbx_asset_regex();
    let asset_url_regex = get_asset_url_regex();
    let raw_large_number_regex = get_raw_large_number_regex();
    rbx_asset_regex.is_match(text)
        || asset_url_regex.is_match(text)
        || raw_large_number_regex.is_match(text)
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
