use std::collections::HashMap;
use std::{env, fs, path::PathBuf};

const DEFAULT_SKIN: &str = "skins/default";

fn main() {
    compile_skin();
    stamp_version();
    embed_client_config();
    windows_resources();
}

fn compile_skin() {
    println!("cargo:rerun-if-env-changed=LAMINARA_SKIN_DIR");
    let here = PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap());
    let skin = env::var("LAMINARA_SKIN_DIR")
        .ok()
        .filter(|dir| !dir.trim().is_empty())
        .map(|dir| here.join(dir))
        .unwrap_or_else(|| here.join(DEFAULT_SKIN));
    let libraries = HashMap::from([
        ("laminara".to_string(), here.join("contract")),
        ("laminara-default".to_string(), here.join(DEFAULT_SKIN)),
    ]);
    let config = slint_build::CompilerConfiguration::new().with_library_paths(libraries);
    slint_build::compile_with_config(skin.join("main.slint"), config)
        .unwrap_or_else(|error| panic!("скин {}: {error}", skin.display()));
}

fn stamp_version() {
    let file = PathBuf::from("../../VERSION");
    let version = fs::read_to_string(&file)
        .map(|text| text.trim().to_string())
        .unwrap_or_else(|_| env::var("CARGO_PKG_VERSION").unwrap());
    println!("cargo:rustc-env=LAMINARA_VERSION={version}");
    println!("cargo:rerun-if-changed=../../VERSION");
}

fn embed_client_config() {
    let out = PathBuf::from(env::var("OUT_DIR").unwrap()).join("embedded_client_config.json");
    let contents = match env::var("LAMINARA_CLIENT_CONFIG") {
        Ok(path) => {
            println!("cargo:rerun-if-changed={path}");
            fs::read_to_string(&path)
                .unwrap_or_else(|e| panic!("LAMINARA_CLIENT_CONFIG {path}: {e}"))
        }
        Err(_) => fs::read_to_string("laminara.client.default.json")
            .expect("bundled default client config"),
    };
    fs::write(&out, contents).expect("write embedded client config");
    println!("cargo:rerun-if-env-changed=LAMINARA_CLIENT_CONFIG");
    println!("cargo:rerun-if-changed=laminara.client.default.json");
}

fn windows_resources() {
    println!("cargo:rerun-if-env-changed=LAMINARA_PRODUCT_NAME");
    println!("cargo:rerun-if-env-changed=LAMINARA_PUBLISHER");
    println!("cargo:rerun-if-changed=icons/icon.ico");
    println!("cargo:rerun-if-changed=windows.manifest");
    if env::var("CARGO_CFG_TARGET_OS").as_deref() != Ok("windows") {
        return;
    }
    let product = env::var("LAMINARA_PRODUCT_NAME").unwrap_or_else(|_| "Laminara".into());
    let publisher = env::var("LAMINARA_PUBLISHER").unwrap_or_else(|_| product.clone());
    let mut resource = winresource::WindowsResource::new();
    resource
        .set_icon("icons/icon.ico")
        .set_manifest_file("windows.manifest")
        .set("ProductName", &product)
        .set("FileDescription", &product)
        .set("CompanyName", &publisher)
        .set("OriginalFilename", "laminara.exe");
    resource.compile().expect("windows resources");
}
