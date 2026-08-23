use std::path::PathBuf;

fn main() {
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
