pub mod profile;

use std::io::Read;
use std::path::Path;

use crate::account::GameSession;
use crate::error::CoreError;
use crate::features::LaunchExtras;
use profile::LaunchProfile;

pub const LAUNCH_PROFILE_NAME: &str = "laminara.profile.json";

pub struct LaunchInputs<'a> {
    pub profile: &'a LaunchProfile,
    pub profile_dir: &'a Path,
    pub game_dir: &'a Path,
    pub java_bin: &'a Path,
    pub natives_dir: &'a Path,
    pub yggdrasil_root: &'a str,
    pub authlib_jar: &'a Path,
    pub prefetch_b64: &'a str,
    pub session: &'a GameSession,
    pub jvm_tuning: &'a [String],
    pub extras: &'a LaunchExtras,
    pub client_version: &'a str,
}

fn separator(os: &str) -> &'static str {
    if os == "windows" {
        ";"
    } else {
        ":"
    }
}

fn version_id(profile: &LaunchProfile) -> String {
    if !profile.version_id.is_empty() {
        return profile.version_id.clone();
    }
    profile
        .client_jar
        .rsplit('/')
        .next()
        .and_then(|f| f.strip_suffix(".jar"))
        .unwrap_or("client")
        .to_string()
}

fn substitute(arg: &str, libraries_dir: &str, sep: &str, version: &str) -> String {
    arg.replace("${library_directory}", libraries_dir)
        .replace("${classpath_separator}", sep)
        .replace("${version_name}", version)
}

pub fn build_argv(input: &LaunchInputs) -> Vec<String> {
    let profile = input.profile;
    let sep = separator(&profile.os);
    let version = version_id(profile);
    let join = |rel: &str| input.profile_dir.join(rel).to_string_lossy().into_owned();
    let libraries_dir = join("libraries");
    let natives_dir = input.natives_dir.to_string_lossy().into_owned();

    let mut argv: Vec<String> = vec![input.java_bin.to_string_lossy().into_owned()];

    if profile.os == "osx" || profile.os == "macos" {
        argv.push("-XstartOnFirstThread".into());
    }
    argv.push(format!("-Djava.library.path={natives_dir}"));
    argv.push(format!(
        "-Dorg.lwjgl.system.SharedLibraryExtractPath={natives_dir}"
    ));
    argv.push("-Dminecraft.launcher.brand=laminara".into());
    argv.push(format!(
        "-Dminecraft.launcher.version={}",
        input.client_version
    ));
    argv.extend(input.jvm_tuning.iter().cloned());

    argv.push(format!(
        "-javaagent:{}={}",
        input.authlib_jar.to_string_lossy(),
        input.yggdrasil_root
    ));
    argv.push("-Dauthlibinjector.side=client".into());
    argv.push(format!(
        "-Dauthlibinjector.yggdrasil.prefetched={}",
        input.prefetch_b64
    ));

    let classpath = profile
        .classpath
        .iter()
        .chain(input.extras.classpath.iter())
        .map(|entry| join(entry))
        .collect::<Vec<_>>()
        .join(sep);
    argv.push("-cp".into());
    argv.push(classpath);

    for arg in profile.jvm_args.iter().chain(input.extras.jvm_args.iter()) {
        argv.push(substitute(arg, &libraries_dir, sep, &version));
    }

    argv.push(profile.main_class.clone());

    let session = input.session;
    argv.extend([
        "--username".into(),
        session.name.clone(),
        "--version".into(),
        version.clone(),
        "--gameDir".into(),
        input.game_dir.to_string_lossy().into_owned(),
        "--assetsDir".into(),
        join("assets"),
        "--assetIndex".into(),
        profile.asset_index.clone(),
        "--uuid".into(),
        session.uuid.clone(),
        "--accessToken".into(),
        session.access_token.clone(),
        "--clientId".into(),
        String::new(),
        "--xuid".into(),
        String::new(),
        "--userType".into(),
        "msa".into(),
        "--versionType".into(),
        "release".into(),
    ]);

    for arg in profile
        .game_args
        .iter()
        .chain(input.extras.game_args.iter())
    {
        argv.push(substitute(arg, &libraries_dir, sep, &version));
    }

    argv
}

pub fn extract_natives(
    profile: &LaunchProfile,
    profile_dir: &Path,
    natives_dir: &Path,
) -> Result<(), CoreError> {
    if natives_dir.exists() {
        std::fs::remove_dir_all(natives_dir)
            .map_err(|e| CoreError::Launch(format!("clear natives: {e}")))?;
    }
    std::fs::create_dir_all(natives_dir)
        .map_err(|e| CoreError::Launch(format!("create natives: {e}")))?;

    for entry in &profile.natives {
        let jar_path = profile_dir.join(entry);
        let file = std::fs::File::open(&jar_path)
            .map_err(|e| CoreError::Launch(format!("open native {}: {e}", jar_path.display())))?;
        let mut archive = zip::ZipArchive::new(file)
            .map_err(|e| CoreError::Launch(format!("read native jar: {e}")))?;
        for index in 0..archive.len() {
            let mut member = archive
                .by_index(index)
                .map_err(|e| CoreError::Launch(e.to_string()))?;
            if member.is_dir() {
                continue;
            }
            let name = member.name().to_string();
            if name.starts_with("META-INF/") {
                continue;
            }
            let file_name = match Path::new(&name).file_name() {
                Some(n) => n.to_owned(),
                None => continue,
            };
            let dest = natives_dir.join(file_name);
            let mut buffer = Vec::with_capacity(member.size() as usize);
            member
                .read_to_end(&mut buffer)
                .map_err(|e| CoreError::Launch(e.to_string()))?;
            std::fs::write(&dest, &buffer).map_err(|e| CoreError::Launch(e.to_string()))?;
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn session() -> GameSession {
        GameSession {
            uuid: "0af1c2d3e4f5".into(),
            name: "Neo".into(),
            access_token: "AT".into(),
            client_token: "CT".into(),
        }
    }

    fn base_profile() -> LaunchProfile {
        LaunchProfile {
            main_class: "net.minecraft.client.main.Main".into(),
            java_component: "java-runtime-delta".into(),
            java_major: 21,
            os: "windows".into(),
            arch: "x86_64".into(),
            platform_key: "windows-x64".into(),
            java_bin: String::new(),
            version_id: "1.21.1".into(),
            asset_index: "17".into(),
            client_jar: "versions/1.21.1/1.21.1.jar".into(),
            classpath: vec![
                "libraries/a/a.jar".into(),
                "versions/1.21.1/1.21.1.jar".into(),
            ],
            natives: vec![],
            jvm_args: vec![],
            game_args: vec![],
            runtime: "runtime/windows-x64".into(),
        }
    }

    fn no_extras() -> LaunchExtras {
        LaunchExtras::default()
    }

    fn inputs<'a>(
        profile: &'a LaunchProfile,
        session: &'a GameSession,
        java: &'a PathBuf,
        natives: &'a PathBuf,
        jar: &'a PathBuf,
        dir: &'a PathBuf,
        game: &'a PathBuf,
        tuning: &'a [String],
        extras: &'a LaunchExtras,
    ) -> LaunchInputs<'a> {
        LaunchInputs {
            profile,
            profile_dir: dir,
            game_dir: game,
            java_bin: java,
            natives_dir: natives,
            yggdrasil_root: "https://eu-1.example.net/yggdrasil/",
            authlib_jar: jar,
            prefetch_b64: "PREFETCH",
            session,
            jvm_tuning: tuning,
            extras,
            client_version: "0.1.0",
        }
    }

    #[test]
    fn vanilla_argv_supplies_base() {
        let profile = base_profile();
        let session = session();
        let (java, natives, jar, dir, game) = (
            PathBuf::from("/p/runtime/windows-x64/bin/java"),
            PathBuf::from("/p/natives/windows-x64"),
            PathBuf::from("/res/authlib-injector.jar"),
            PathBuf::from("/p"),
            PathBuf::from("/p"),
        );
        let tuning = vec!["-Xmx4G".to_string()];
        let argv = build_argv(&inputs(
            &profile,
            &session,
            &java,
            &natives,
            &jar,
            &dir,
            &game,
            &tuning,
            &no_extras(),
        ));
        let line = argv.join(" ");

        assert_eq!(argv[0], "/p/runtime/windows-x64/bin/java");
        assert!(line.contains("-Xmx4G"));
        assert!(line
            .contains("-javaagent:/res/authlib-injector.jar=https://eu-1.example.net/yggdrasil/"));
        assert!(line.contains("-Dauthlibinjector.yggdrasil.prefetched=PREFETCH"));
        assert!(line.contains("net.minecraft.client.main.Main"));
        assert!(line.contains("--username Neo"));
        assert!(line.contains("--accessToken AT"));
        assert!(line.contains("--uuid 0af1c2d3e4f5"));
        assert!(line.contains("--userType msa"));
        let cp = argv.iter().position(|a| a == "-cp").unwrap();
        assert!(argv[cp + 1].contains(";"), "windows classpath separator");
        assert!(
            argv[cp + 1].contains("versions/1.21.1/1.21.1.jar")
                || argv[cp + 1].contains("versions\\1.21.1")
        );
    }

    #[test]
    fn neoforge_appends_loader_extras_with_substitution() {
        let mut profile = base_profile();
        profile.main_class = "cpw.mods.bootstraplauncher.BootstrapLauncher".into();
        profile.jvm_args = vec![
            "-DlibraryDirectory=${library_directory}".into(),
            "-p".into(),
            "${library_directory}/cpw/mods/bootstraplauncher/2.0.2/bootstraplauncher-2.0.2.jar${classpath_separator}${library_directory}/cpw/mods/securejarhandler/3.0.8/securejarhandler-3.0.8.jar".into(),
            "--add-modules".into(),
            "ALL-MODULE-PATH".into(),
            "-DignoreList=client-extra,${version_name}.jar".into(),
        ];
        profile.game_args = vec![
            "--fml.neoForgeVersion".into(),
            "21.1.235".into(),
            "--fml.mcVersion".into(),
            "1.21.1".into(),
            "--launchTarget".into(),
            "forgeclient".into(),
        ];
        let session = session();
        let (java, natives, jar, dir, game) = (
            PathBuf::from("/p/runtime/windows-x64/bin/java"),
            PathBuf::from("/p/natives/windows-x64"),
            PathBuf::from("/res/authlib-injector.jar"),
            PathBuf::from("/p"),
            PathBuf::from("/p"),
        );
        let argv = build_argv(&inputs(
            &profile,
            &session,
            &java,
            &natives,
            &jar,
            &dir,
            &game,
            &[],
            &no_extras(),
        ));
        let line = argv.join(" ");

        let libraries = dir.join("libraries").to_string_lossy().into_owned();
        assert!(line.contains("cpw.mods.bootstraplauncher.BootstrapLauncher"));
        assert!(line.contains(&format!("-DlibraryDirectory={libraries}")));
        assert!(line.contains("-DignoreList=client-extra,1.21.1.jar"));
        assert!(line.contains(&format!(
            "bootstraplauncher-2.0.2.jar;{libraries}/cpw/mods/securejarhandler"
        )));
        assert!(line.contains("--add-modules ALL-MODULE-PATH"));
        assert!(line.contains("--launchTarget forgeclient"));
        assert!(line.contains("--fml.mcVersion 1.21.1"));
        assert!(!line.contains("${"), "no unsubstituted placeholders remain");
    }

    #[test]
    fn optional_mods_extend_the_launch_command() {
        let mut profile = base_profile();
        profile.jvm_args = vec!["-Dprofile.flag=1".into()];
        profile.game_args = vec!["--profileArg".into()];
        let session = session();
        let (java, natives, jar, dir, game) = (
            PathBuf::from("/p/runtime/windows-x64/bin/java"),
            PathBuf::from("/p/natives/windows-x64"),
            PathBuf::from("/res/authlib-injector.jar"),
            PathBuf::from("/p"),
            PathBuf::from("/p"),
        );
        let extras = LaunchExtras {
            jvm_args: vec!["-Diris.enable=true".into()],
            game_args: vec!["--zoom".into()],
            classpath: vec!["mods/iris.jar".into()],
        };
        let argv = build_argv(&inputs(
            &profile,
            &session,
            &java,
            &natives,
            &jar,
            &dir,
            &game,
            &[],
            &extras,
        ));
        let at = |needle: &str| {
            argv.iter()
                .position(|a| a == needle)
                .unwrap_or_else(|| panic!("{needle} is missing from {argv:?}"))
        };
        let main_class = at(&profile.main_class);
        let cp = at("-cp");

        assert!(at("-Dprofile.flag=1") < at("-Diris.enable=true"));
        assert!(at("-Diris.enable=true") < main_class);
        assert!(at("--profileArg") < at("--zoom"));
        assert!(main_class < at("--profileArg"));

        let classpath: Vec<&str> = argv[cp + 1].split(';').collect();
        assert_eq!(classpath.len(), 3);
        assert!(classpath[2].ends_with("mods/iris.jar"));
    }
}
