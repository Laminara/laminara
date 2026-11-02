use std::path::PathBuf;

use ed25519_dalek::VerifyingKey;
use laminara_core::{features::FeatureSelection, manifest, rpc::LauncherClient, sync, transport};
use serde::Deserialize;
use tokio_util::sync::CancellationToken;

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Fixture {
    endpoint: String,
    username: String,
    password: String,
    public_key: String,
    profile: String,
    root: PathBuf,
    optional_group: String,
    optional_file: String,
    expected_marker: String,
}

#[tokio::test]
#[ignore = "requires an isolated server and LAMINARA_SYNC_FIXTURE JSON"]
async fn signed_build_sync_optional_mods_and_cache() {
    let config = std::env::var("LAMINARA_SYNC_FIXTURE").expect("fixture path");
    let fixture: Fixture = serde_json::from_slice(&std::fs::read(config).unwrap()).unwrap();
    assert!(fixture.root.is_absolute());
    let transport = transport::Transport::new(transport::default_http_client());
    let client = LauncherClient::new(&transport, &fixture.endpoint);
    let login = client
        .login(fixture.username, fixture.password, String::new(), None)
        .await
        .unwrap();
    transport.set_access_token(Some(login.tokens.unwrap().access));
    assert!(client
        .list_profiles()
        .await
        .unwrap()
        .iter()
        .any(|p| p.name == fixture.profile));
    let response = client.get_manifest(fixture.profile).await.unwrap();
    let key: [u8; 32] = hex::decode(fixture.public_key).unwrap().try_into().unwrap();
    let verified = manifest::verify_and_decode(
        &[VerifyingKey::from_bytes(&key).unwrap()],
        &response.manifest,
        &response.signature,
    )
    .unwrap();
    let profile = fixture.root.join("profile");
    let state = fixture.root.join("state");
    let cas = fixture.root.join("cas");
    let mut selection = FeatureSelection::default();
    for phase in 0..4 {
        if phase == 2 {
            selection
                .selected
                .insert(fixture.optional_group.clone(), Vec::new());
        } else {
            selection.selected.clear();
        }
        let outcome = sync::sync(
            sync::SyncPlan {
                transport: &transport,
                base_url: &fixture.endpoint,
                profile_dir: &profile,
                state_dir: &state,
                cas_dir: &cas,
                manifest: &verified.manifest,
                selection: &selection,
                max_parallel: 8,
                cancel: CancellationToken::new(),
            },
            |_| {},
        )
        .await
        .unwrap();
        println!("phase {phase}: {outcome:?}");
        if phase > 0 {
            assert_eq!(outcome.downloaded, 0);
        }
        assert_eq!(profile.join(&fixture.optional_file).exists(), phase != 2);
        assert!(sync::verify_contents(&profile, &state).is_empty());
        assert_eq!(
            std::fs::read_to_string(profile.join("acceptance-release.txt"))
                .unwrap()
                .trim(),
            fixture.expected_marker
        );
        if phase == 0 && !profile.join("options.txt").exists() {
            std::fs::write(profile.join("options.txt"), "player-owned-settings").unwrap();
        }
        assert_eq!(
            std::fs::read_to_string(profile.join("options.txt")).unwrap(),
            "player-owned-settings"
        );
    }
}
