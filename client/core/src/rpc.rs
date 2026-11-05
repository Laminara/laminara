use crate::error::RpcError;
use crate::proto::api::v1::{
    CheckUpdateRequest, CheckUpdateResponse, GetChallengeRequest, GetChallengeResponse,
    GetManifestRequest, GetManifestResponse, GetNewsRequest, GetNewsResponse, ListProfilesRequest,
    ListProfilesResponse, LoginRequest, LoginResponse, MachineReport, NewsItem, ProfileSummary,
    RefreshRequest, RefreshResponse, ReportCrashRequest, ReportCrashResponse,
    ReportLauncherLogRequest, ReportLauncherLogResponse, ReportMachineRequest,
    ReportMachineResponse, Tokens,
};
use crate::proto::api::v1::{CrashReport, LauncherLogReport};
use crate::transport::Transport;

const SERVICE: &str = "laminara.api.v1.LauncherService";

pub struct LauncherClient<'a> {
    transport: &'a Transport,
    base_url: &'a str,
}

impl<'a> LauncherClient<'a> {
    pub async fn account_policy(&self) -> Result<crate::proto::api::v1::AccountPolicy, RpcError> {
        let response: Result<crate::proto::api::v1::GetAccountPolicyResponse, RpcError> = self
            .transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/GetAccountPolicy"),
                &crate::proto::api::v1::GetAccountPolicyRequest {},
            )
            .await;
        let response = match response {
            Err(RpcError::PostSend(message)) if message == "http 404 Not Found" => {
                return Ok(Default::default())
            }
            result => result?,
        };
        Ok(response.policy.unwrap_or_default())
    }

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
        two_factor_code: String,
        machine: Option<MachineReport>,
    ) -> Result<LoginResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/Login"),
                &LoginRequest {
                    username,
                    password,
                    two_factor_code,
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

    pub async fn report_crash(&self, crash: CrashReport) -> Result<ReportCrashResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/ReportCrash"),
                &ReportCrashRequest { crash: Some(crash) },
            )
            .await
    }

    pub async fn report_launcher_log(
        &self,
        report: LauncherLogReport,
    ) -> Result<ReportLauncherLogResponse, RpcError> {
        self.transport
            .unary(
                self.base_url,
                &format!("{SERVICE}/ReportLauncherLog"),
                &ReportLauncherLogRequest {
                    report: Some(report),
                },
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
mod tests {
    use super::*;
    use std::io::{Read, Write};

    async fn policy_reply(
        status: &str,
        body: Vec<u8>,
    ) -> Result<crate::proto::api::v1::AccountPolicy, RpcError> {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let status = status.to_string();
        let responder = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream
                .set_read_timeout(Some(std::time::Duration::from_secs(5)))
                .unwrap();
            let mut request = [0; 4096];
            stream.read(&mut request).unwrap();
            write!(
                stream,
                "HTTP/1.1 {status}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                body.len()
            )
            .unwrap();
            stream.write_all(&body).unwrap();
        });
        let transport = Transport::new(reqwest::Client::builder().no_proxy().build().unwrap());
        let result = LauncherClient::new(&transport, &url).account_policy().await;
        responder.join().unwrap();
        result
    }

    #[tokio::test]
    async fn old_servers_keep_single_account_mode() {
        let policy = policy_reply("404 Not Found", b"404 page not found".to_vec())
            .await
            .unwrap();
        assert!(!policy.multi_account);
    }

    #[tokio::test]
    async fn server_failure_is_not_a_default_policy() {
        assert!(policy_reply("503 Service Unavailable", Vec::new())
            .await
            .is_err());
    }

    #[tokio::test]
    async fn policy_reads_the_server_limit() {
        use crate::proto::api::v1::{AccountPolicy, GetAccountPolicyResponse};
        use prost::Message;
        let expected = AccountPolicy {
            multi_account: true,
            max_accounts: 3,
        };
        let body = GetAccountPolicyResponse {
            policy: Some(expected.clone()),
        }
        .encode_to_vec();
        assert_eq!(policy_reply("200 OK", body).await.unwrap(), expected);
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
            .login("neo".into(), "matrix".into(), String::new(), None)
            .await
            .expect("login")
            .tokens
            .expect("tokens");
        assert!(!tokens.access.is_empty(), "empty access token");
        assert!(!tokens.refresh.is_empty(), "empty refresh token");

        let profiles = client.list_profiles().await.expect("list_profiles");
        eprintln!("live: access ok, {} profiles", profiles.len());

        let bad = client
            .login("neo".into(), "wrong".into(), String::new(), None)
            .await;
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
                    String::new(),
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
