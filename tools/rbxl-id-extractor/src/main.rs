use std::collections::{BTreeMap, BTreeSet};
use std::env;
use std::fs::File;
use std::io::BufReader;
use std::sync::OnceLock;

use rbx_binary::from_reader;
use regex::Regex;
use serde::Serialize;

const RAW_ASSET_CONTEXT_WINDOW: usize = 80;
const PHYSICS_DATA_PROPERTY_TOKEN: &str = "physicsdata";
const IMAGE_PROPERTY_TOKENS: [&str; 9] = [
    "texture",
    "image",
    "decal",
    "thumbnail",
    "shirt",
    "pants",
    "face",
    "icon",
    "content",
];

#[derive(Serialize)]
struct ExtractResult {
    asset_ids: Vec<i64>,
    asset_use_counts: BTreeMap<i64, usize>,
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
    let mut extracted_asset_ids = BTreeSet::<i64>::new();
    let mut asset_use_counts = BTreeMap::<i64, usize>::new();
    let file = File::open(file_path).map_err(|open_err| format!("open failed: {}", open_err))?;
    if let Ok(dom) = from_reader(BufReader::new(file)) {
        for instance in dom.descendants() {
            let meshpart_has_surface_color_map =
                normalize_for_match(instance.class.as_ref()) == "meshpart"
                    && instance.children().iter().any(|child_ref| {
                        if let Some(child_instance) = dom.get_by_ref(*child_ref) {
                            if normalize_for_match(child_instance.class.as_ref())
                                != "surfaceappearance"
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
                                let rendered_color_map_value =
                                    format!("{:?}", child_property_value);
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
                if !is_image_property_name(&normalized_property_name) {
                    continue;
                }
                extract_ids_from_text(
                    &rendered_property_value,
                    &mut extracted_asset_ids,
                    &mut asset_use_counts,
                );
            }
        }
    } else {
        extract_ids_from_text(
            &String::from_utf8_lossy(&file_bytes),
            &mut extracted_asset_ids,
            &mut asset_use_counts,
        );
    }

    let asset_ids = if let Some(max_results) = max_results {
        extracted_asset_ids
            .into_iter()
            .take(max_results)
            .collect::<Vec<_>>()
    } else {
        extracted_asset_ids.into_iter().collect::<Vec<_>>()
    };
    let mut limited_use_counts = BTreeMap::<i64, usize>::new();
    for asset_id in &asset_ids {
        if let Some(use_count) = asset_use_counts.get(asset_id) {
            limited_use_counts.insert(*asset_id, *use_count);
        }
    }
    let output = serde_json::to_string(&ExtractResult {
        asset_ids,
        asset_use_counts: limited_use_counts,
    })
        .map_err(|json_err| format!("json failed: {}", json_err))?;
    println!("{}", output);
    Ok(())
}

fn extract_ids_from_text(
    text: &str,
    extracted_asset_ids: &mut BTreeSet<i64>,
    asset_use_counts: &mut BTreeMap<i64, usize>,
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
                extracted_asset_ids.insert(asset_id);
                increment_use_count(asset_use_counts, asset_id);
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
            extracted_asset_ids.insert(asset_id);
            increment_use_count(asset_use_counts, asset_id);
        }
    }
}

fn increment_use_count(asset_use_counts: &mut BTreeMap<i64, usize>, asset_id: i64) {
    let entry = asset_use_counts.entry(asset_id).or_insert(0);
    *entry += 1;
}

fn range_overlaps_explicit(start: usize, end: usize, explicit_ranges: &[(usize, usize)]) -> bool {
    for (explicit_start, explicit_end) in explicit_ranges {
        if start < *explicit_end && *explicit_start < end {
            return true;
        }
    }
    false
}

fn is_image_property_name(normalized_property_name: &str) -> bool {
    for image_property_token in IMAGE_PROPERTY_TOKENS {
        if normalized_property_name.contains(image_property_token) {
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
        Regex::new(r"(?i)(texture|image|decal|thumbnail|shirt|pants|face|icon|content)")
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
