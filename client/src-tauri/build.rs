use std::{env, fs, path::PathBuf};

fn main() {
    tauri_build::build();
    stamp_version();

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

fn stamp_version() {
    let file = PathBuf::from("../../VERSION");
    let version = fs::read_to_string(&file)
        .map(|text| text.trim().to_string())
        .unwrap_or_else(|_| env::var("CARGO_PKG_VERSION").unwrap());
    println!("cargo:rustc-env=LAMINARA_VERSION={version}");
    println!("cargo:rerun-if-changed=../../VERSION");
}
