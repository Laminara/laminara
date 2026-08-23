use crate::error::RpcError;
use crate::proto::api::v1::{
    CheckUpdateRequest, CheckUpdateResponse, GetChallengeRequest, GetChallengeResponse,
    GetManifestRequest, GetManifestResponse, GetNewsRequest, GetNewsResponse, ListProfilesRequest,
    ListProfilesResponse, LoginRequest, LoginResponse, MachineReport, NewsItem, ProfileSummary,
    RefreshRequest, RefreshResponse, ReportMachineRequest, ReportMachineResponse, Tokens,
};
use crate::transport::Transport;

const SERVICE: &str = "laminara.api.v1.LauncherService";

pub struct LauncherClient<'a> {
    transport: &'a Transport,
    base_url: &'a str,
}

impl<'a> LauncherClient<'a> {
    pub fn new(transport: &'a Transport, base_url: &'a str) -> Self {
        Self {
            transport,
            base_url,
        }
    }

    pub async fn login(
        &self,
        username: String,
        password: String,
        machine: Option<MachineReport>,
    ) -> Result<LoginResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/Login"),
                &LoginRequest {
                    username,
                    password,
                    machine,
                },
            )
            .await
    }

    pub async fn challenge(&self) -> Result<GetChallengeResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/GetChallenge"),
                &GetChallengeRequest {},
            )
            .await
    }

    pub async fn report_machine(
        &self,
        machine: MachineReport,
    ) -> Result<ReportMachineResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/ReportMachine"),
                &ReportMachineRequest {
                    machine: Some(machine),
                },
            )
            .await
    }

    pub async fn refresh(&self, refresh: String) -> Result<Tokens, RpcError> {
        let response: RefreshResponse = self
            .transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/Refresh"),
                &RefreshRequest { refresh },
            )
            .await?;
        response
            .tokens
            .ok_or_else(|| RpcError::PostSend("refresh response missing tokens".into()))
    }

    pub async fn list_profiles(&self) -> Result<Vec<ProfileSummary>, RpcError> {
        let response: ListProfilesResponse = self
            .transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/ListProfiles"),
                &ListProfilesRequest {
                    platform: crate::platform::current() as i32,
                },
            )
            .await?;
        Ok(response.profiles)
    }

    pub async fn check_update(
        &self,
        current_version: String,
    ) -> Result<CheckUpdateResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/CheckUpdate"),
                &CheckUpdateRequest {
                    current_version,
                    platform: crate::platform::current() as i32,
                },
            )
            .await
    }

    pub async fn news(&self) -> Result<Vec<NewsItem>, RpcError> {
        let response: GetNewsResponse = self
            .transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/GetNews"),
                &GetNewsRequest {},
            )
            .await?;
        Ok(response.items)
    }

    pub async fn get_manifest(&self, profile: String) -> Result<GetManifestResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/GetManifest"),
                &GetManifestRequest {
                    profile,
                    platform: crate::platform::current() as i32,
                },
            )
            .await
    }
}

#[cfg(test)]
mod live {
    use super::*;
    use crate::transport::Transport;

    #[tokio::test]
    async fn login_and_list_profiles() {
        if std::env::var("LAMINARA_CLIENT_E2E").is_err() {
            return;
        }
        let base =
            std::env::var("LAMINARA_BASE").unwrap_or_else(|_| "http://127.0.0.1:8099".into());
        let transport = Transport::default();
        let client = LauncherClient::new(&transport, &base);

        let tokens = client
            .login("neo".into(), "matrix".into(), None)
            .await
            .expect("login")
            .tokens
            .expect("tokens");
        assert!(!tokens.access.is_empty(), "empty access token");
        assert!(!tokens.refresh.is_empty(), "empty refresh token");

        let profiles = client.list_profiles().await.expect("list_profiles");
        eprintln!("live: access ok, {} profiles", profiles.len());

        let bad = client.login("neo".into(), "wrong".into(), None).await;
        assert!(
            matches!(bad, Err(RpcError::App { .. })),
            "wrong password should be App error, got {bad:?}"
        );
    }

    #[tokio::test]
    async fn one_computer_two_accounts_is_one_machine() {
        if std::env::var("LAMINARA_CLIENT_E2E").is_err() {
            return;
        }
        let Ok(salt_path) = std::env::var("LAMINARA_HWID_SALT_FILE") else {
            return;
        };
        let salt: [u8; 32] = std::fs::read(salt_path).expect("salt file")[..32]
            .try_into()
            .expect("32-byte salt");
        let base =
            std::env::var("LAMINARA_BASE").unwrap_or_else(|_| "http://127.0.0.1:8099".into());
        let transport = Transport::default();
        let client = LauncherClient::new(&transport, &base);
        let facts = crate::machine::MachineFacts::collect(&salt).await;

        let mut machines = Vec::new();
        for user in ["neo", "trinity"] {
            let nonce = client.challenge().await.expect("challenge").nonce;
            assert!(
                !nonce.is_empty(),
                "the server must issue a challenge when hwid is on"
            );
            let response = client
                .login(
                    user.into(),
                    "matrix".into(),
                    Some(facts.report(nonce, "0.1.0-test")),
                )
                .await
                .unwrap_or_else(|e| panic!("login as {user}: {e}"));
            let verdict = response
                .machine
                .expect("the server must return a machine verdict");
            eprintln!(
                "live: {user} -> machine {} (first_seen={})",
                verdict.machine_id, verdict.first_seen
            );
            assert!(
                !verdict.machine_ticket.is_empty(),
                "a verdict must carry an in-game ticket"
            );
            machines.push(verdict.machine_id);
        }
        assert_eq!(
            machines[0], machines[1],
            "two accounts on one computer must resolve to one machine"
        );
    }
}
