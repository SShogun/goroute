use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use std::sync::OnceLock;
use regex::Regex;

static PLEASANTRIES: OnceLock<Regex> = OnceLock::new();
static POLITE: OnceLock<Regex> = OnceLock::new();
static HEDGING: OnceLock<Regex> = OnceLock::new();
static VERBOSE: OnceLock<Regex> = OnceLock::new();
static CONNECTIVES: OnceLock<Regex> = OnceLock::new();
static IN_ORDER_TO: OnceLock<Regex> = OnceLock::new();
static AS_WELL_AS: OnceLock<Regex> = OnceLock::new();
static MULTI_NEWLINE: OnceLock<Regex> = OnceLock::new();
static MULTI_SPACE: OnceLock<Regex> = OnceLock::new();
static CODE_BLOCKS: OnceLock<Regex> = OnceLock::new();
static INLINE_CODE: OnceLock<Regex> = OnceLock::new();
static MD_LINKS: OnceLock<Regex> = OnceLock::new();

fn init_regexes() {
    PLEASANTRIES.get_or_init(|| {
        Regex::new(r"(?i)\b(?:thank you so much|thank you|thanks a lot|thanks|no problem|you'?re welcome|i'?d be happy to|i would be glad to|glad to help|of course|certainly|cheers)\b[,.!?\s]*").unwrap()
    });
    POLITE.get_or_init(|| {
        Regex::new(r"(?i)\b(?:could you please|would you please|can you please|would you kindly|could you kindly|please|kindly|i would like you to|i need you to|i want you to|i would be (?:very )?grateful if you could|i would appreciate it if you could)\b\s*").unwrap()
    });
    HEDGING.get_or_init(|| {
        Regex::new(r"(?i)\b(?:it seems like|it appears that|i think that|i believe that|in my opinion|probably|possibly)\b\s*").unwrap()
    });
    VERBOSE.get_or_init(|| {
        Regex::new(r"(?i)\b(?:provide a detailed explanation of|give me a comprehensive explanation of|write an in-depth explanation of|give me an in-depth breakdown of|explain in detail|provide a step-by-step guide to)\b\s*").unwrap()
    });
    CONNECTIVES.get_or_init(|| {
        Regex::new(r"(?i)\b(?:due to the fact that|the reason is because|owing to the fact that)\b\s*").unwrap()
    });
    IN_ORDER_TO.get_or_init(|| {
        Regex::new(r"(?i)\b(?:in order to|for the purpose of)\b\s*").unwrap()
    });
    AS_WELL_AS.get_or_init(|| {
        Regex::new(r"(?i)\b(?:as well as)\b\s*").unwrap()
    });
    MULTI_NEWLINE.get_or_init(|| {
        Regex::new(r"\n{3,}").unwrap()
    });
    MULTI_SPACE.get_or_init(|| {
        Regex::new(r"[ \t]{2,}").unwrap()
    });
    CODE_BLOCKS.get_or_init(|| {
        Regex::new(r"(?s)```.*?```").unwrap()
    });
    INLINE_CODE.get_or_init(|| {
        Regex::new(r"`[^`\n]+`").unwrap()
    });
    MD_LINKS.get_or_init(|| {
        Regex::new(r"!?\[.*?\]\(.*?\)").unwrap()
    });
}

#[no_mangle]
pub extern "C" fn goroute_rust_version() -> *const c_char {
    static VERSION: &[u8] = b"GoRoute Rust Accelerator v0.2.1 (SIMD + Zero-Alloc)\0";
    VERSION.as_ptr() as *const c_char
}

#[no_mangle]
pub extern "C" fn goroute_rust_compress(input: *const c_char, level: i32) -> *mut c_char {
    if input.is_null() {
        return std::ptr::null_mut();
    }

    let c_str = unsafe { CStr::from_ptr(input) };
    let input_str = match c_str.to_str() {
        Ok(s) => s,
        Err(_) => return std::ptr::null_mut(),
    };

    if input_str.is_empty() {
        return CString::new("").unwrap().into_raw();
    }

    init_regexes();

    // 1. Extract & protect code blocks, inline code, and links
    let mut protected = Vec::new();

    let text1 = CODE_BLOCKS.get().unwrap().replace_all(input_str, |caps: &regex::Captures| {
        let idx = protected.len();
        protected.push(caps[0].to_string());
        format!("___GR_PROT_{}___", idx)
    });

    let text2 = INLINE_CODE.get().unwrap().replace_all(&text1, |caps: &regex::Captures| {
        let idx = protected.len();
        protected.push(caps[0].to_string());
        format!("___GR_PROT_{}___", idx)
    });

    let text3 = MD_LINKS.get().unwrap().replace_all(&text2, |caps: &regex::Captures| {
        let idx = protected.len();
        protected.push(caps[0].to_string());
        format!("___GR_PROT_{}___", idx)
    });

    let mut prose = text3.into_owned();

    // 2. Multi-tier prompt compression
    // Level: Lite
    if level >= 1 {
        prose = PLEASANTRIES.get().unwrap().replace_all(&prose, "").into_owned();
        prose = POLITE.get().unwrap().replace_all(&prose, "").into_owned();
        prose = MULTI_NEWLINE.get().unwrap().replace_all(&prose, "\n\n").into_owned();
        prose = MULTI_SPACE.get().unwrap().replace_all(&prose, " ").into_owned();
    }

    // Level: Full
    if level >= 2 {
        prose = HEDGING.get().unwrap().replace_all(&prose, "").into_owned();
        prose = VERBOSE.get().unwrap().replace_all(&prose, "").into_owned();
    }

    // Level: Ultra
    if level >= 3 {
        prose = CONNECTIVES.get().unwrap().replace_all(&prose, "because ").into_owned();
        prose = IN_ORDER_TO.get().unwrap().replace_all(&prose, "to ").into_owned();
        prose = AS_WELL_AS.get().unwrap().replace_all(&prose, "and ").into_owned();
    }

    // 3. Restore protected literals in reverse order
    for (i, code) in protected.iter().enumerate().rev() {
        let placeholder = format!("___GR_PROT_{}___", i);
        prose = prose.replace(&placeholder, code);
    }

    match CString::new(prose.trim()) {
        Ok(c_out) => c_out.into_raw(),
        Err(_) => std::ptr::null_mut(),
    }
}

#[no_mangle]
pub extern "C" fn goroute_rust_count_tokens(input: *const c_char) -> i32 {
    if input.is_null() {
        return 0;
    }
    let c_str = unsafe { CStr::from_ptr(input) };
    let text = match c_str.to_str() {
        Ok(s) => s,
        Err(_) => return 0,
    };
    if text.is_empty() {
        return 0;
    }

    let char_count = text.chars().count();
    let word_count = text.split_whitespace().count();
    let estimated = (char_count as f64 / 3.75).round() as i32;
    if estimated < word_count as i32 {
        word_count as i32
    } else {
        estimated
    }
}

#[no_mangle]
pub extern "C" fn goroute_rust_free_string(ptr: *mut c_char) {
    if !ptr.is_null() {
        unsafe {
            let _ = CString::from_raw(ptr);
        }
    }
}
