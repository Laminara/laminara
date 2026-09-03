use std::path::PathBuf;

fn main() {
    stamp_version();

    let proto_root = PathBuf::from("../../proto");
    let files = [
        "laminara/core/v1/platform.proto",
        "laminara/core/v1/core.proto",
        "laminara/api/v1/machine.proto",
        "laminara/api/v1/api.proto",
    ];
    let descriptors = protox::compile(files, [&proto_root]).expect("compile protobuf definitions");
    prost_build::Config::new()
        .compile_fds(descriptors)
        .expect("generate prost types");
    println!("cargo:rerun-if-changed=../../proto");
}

fn stamp_version() {
    let file = PathBuf::from("../../VERSION");
    let version = std::fs::read_to_string(&file)
        .map(|text| text.trim().to_string())
        .unwrap_or_else(|_| std::env::var("CARGO_PKG_VERSION").unwrap());
    println!("cargo:rustc-env=LAMINARA_VERSION={version}");
    println!("cargo:rerun-if-changed=../../VERSION");
}
