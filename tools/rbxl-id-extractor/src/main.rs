use std::collections::BTreeSet;
use std::env;
use std::fs::File;
use std::io::BufReader;
use std::sync::OnceLock;

use rbx_binary::from_reader;
use regex::Regex;
use serde::Serialize;

const RAW_ASSET_CONTEXT_WINDOW: usize = 80;
const PHYSICS_DATA_PROPERTY_TOKEN: &str = "physicsdata";

#[derive(Serialize)]
struct ExtractResult {
    asset_ids: Vec<i64>,
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
    extract_ids_from_text(
        &String::from_utf8_lossy(&file_bytes),
        &mut extracted_asset_ids,
        false,
    );

    let file = File::open(file_path).map_err(|open_err| format!("open failed: {}", open_err))?;
    if let Ok(dom) = from_reader(BufReader::new(file)) {
        for instance in dom.descendants() {
            for (property_name, property_value) in instance.properties.iter() {
                let normalized_property_name = normalize_for_match(property_name.as_ref());
                if normalized_property_name.contains(PHYSICS_DATA_PROPERTY_TOKEN) {
                    continue;
                }
                let rendered_property_value = format!("{:?}", property_value);
                extract_ids_from_text(&rendered_property_value, &mut extracted_asset_ids, true);
            }
        }
    }

    let asset_ids = if let Some(max_results) = max_results {
        extracted_asset_ids
            .into_iter()
            .take(max_results)
            .collect::<Vec<_>>()
    } else {
        extracted_asset_ids.into_iter().collect::<Vec<_>>()
    };
    let output = serde_json::to_string(&ExtractResult { asset_ids })
        .map_err(|json_err| format!("json failed: {}", json_err))?;
    println!("{}", output);
    Ok(())
}

fn extract_ids_from_text(
    text: &str,
    extracted_asset_ids: &mut BTreeSet<i64>,
    allow_loose_number_matches: bool,
) {
    let rbx_asset_regex = get_rbx_asset_regex();
    let asset_url_regex = get_asset_url_regex();
    let query_id_regex = get_query_id_regex();
    let raw_large_number_regex = get_raw_large_number_regex();
    let loose_number_regex = get_loose_number_regex();
    let roblox_context_regex = get_roblox_context_regex();

    for regex_match in rbx_asset_regex
        .captures_iter(text)
        .chain(asset_url_regex.captures_iter(text))
        .chain(query_id_regex.captures_iter(text))
    {
        if let Some(asset_id_match) = regex_match.get(1) {
            if let Ok(asset_id) = asset_id_match.as_str().trim().parse::<i64>() {
                extracted_asset_ids.insert(asset_id);
            }
        }
    }

    for number_match in raw_large_number_regex.find_iter(text) {
        let context_start = number_match
            .start()
            .saturating_sub(RAW_ASSET_CONTEXT_WINDOW);
        let context_end = (number_match.end() + RAW_ASSET_CONTEXT_WINDOW).min(text.len());
        let context_bytes = &text.as_bytes()[context_start..context_end];
        let context_text = String::from_utf8_lossy(context_bytes);
        if !roblox_context_regex.is_match(&context_text) {
            continue;
        }
        if let Ok(asset_id) = number_match.as_str().trim().parse::<i64>() {
            extracted_asset_ids.insert(asset_id);
        }
    }

    if allow_loose_number_matches {
        for number_match in loose_number_regex.find_iter(text) {
            if let Ok(asset_id) = number_match.as_str().trim().parse::<i64>() {
                extracted_asset_ids.insert(asset_id);
            }
        }
    }
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

fn get_query_id_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| Regex::new(r"(?i)(?:\?|&)id=(\d+)").expect("valid regex"))
}

fn get_raw_large_number_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| Regex::new(r"\b\d{8,}\b").expect("valid regex"))
}

fn get_loose_number_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| Regex::new(r"\b\d{6,}\b").expect("valid regex"))
}

fn get_roblox_context_regex() -> &'static Regex {
    static REGEX: OnceLock<Regex> = OnceLock::new();
    REGEX.get_or_init(|| {
        Regex::new(
            r"(?i)(rbxassetid|assetid|texture|image|decal|thumbnail|meshid|soundid|mesh|content)",
        )
        .expect("valid regex")
    })
}
