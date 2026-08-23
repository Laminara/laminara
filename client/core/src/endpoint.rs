use std::future::Future;
use std::sync::Arc;
use std::time::{Duration, Instant};

use arc_swap::ArcSwap;
use futures::stream::{FuturesUnordered, StreamExt};

use crate::config::EndpointConfig;
use crate::error::{CoreError, RpcError};
use crate::machine::MachineFacts;
use crate::proto::api::v1::{
    GetManifestResponse, LoginResponse, MachineVerdict, NewsItem, ProfileSummary, Tokens,
};
use crate::rpc::LauncherClient;
use crate::transport::Transport;

#[derive(Debug, Clone)]
pub struct EndpointStatus {
    pub id: String,
    pub base_url: String,
    pub healthy: bool,
    pub latency_ms: Option<u32>,
    pub is_current: bool,
}

pub struct EndpointPool {
    transport: Transport,
    endpoints: Vec<EndpointConfig>,
    current: ArcSwap<Option<String>>,
}

impl EndpointPool {
    pub fn new(transport: Transport, endpoints: Vec<EndpointConfig>) -> Self {
        Self {
            transport,
            endpoints,
            current: ArcSwap::from_pointee(None),
        }
    }

    pub fn current_base_url(&self) -> Option<String> {
        (**self.current.load()).clone()
    }

    pub fn id_for(&self, base_url: &str) -> Option<String> {
        self.endpoints
            .iter()
            .find(|e| e.base_url == base_url)
            .map(|e| e.id.clone())
    }

    pub async fn probe(&self) -> Vec<EndpointStatus> {
        let mut inflight = FuturesUnordered::new();
        for endpoint in &self.endpoints {
            let transport = self.transport.clone();
            let id = endpoint.id.clone();
            let base_url = endpoint.base_url.clone();
            inflight.push(async move {
                let started = Instant::now();
                let client = LauncherClient::new(&transport, &base_url);
                let outcome =
                    tokio::time::timeout(Duration::from_millis(1500), client.list_profiles()).await;
                let (healthy, latency_ms) = match outcome {
                    Ok(Ok(_)) => (true, Some(started.elapsed().as_millis() as u32)),
                    _ => (false, None),
                };
                EndpointStatus {
                    id,
                    base_url,
                    healthy,
                    latency_ms,
                    is_current: false,
                }
            });
        }

        let mut results: Vec<EndpointStatus> = Vec::with_capacity(self.endpoints.len());
        while let Some(status) = inflight.next().await {
            results.push(status);
        }

        let best = results
            .iter()
            .filter(|s| s.healthy)
            .min_by_key(|s| s.latency_ms.unwrap_or(u32::MAX))
            .map(|s| s.base_url.clone());
        if let Some(url) = &best {
            self.current.store(Arc::new(Some(url.clone())));
        }

        results.sort_by_key(|s| {
            self.endpoints
                .iter()
                .position(|e| e.id == s.id)
                .unwrap_or(usize::MAX)
        });
        for status in &mut results {
            status.is_current = best.as_deref() == Some(status.base_url.as_str());
        }
        results
    }

    async fn call<T, F, Fut>(&self, run: F) -> Result<T, CoreError>
    where
        F: Fn(Transport, String) -> Fut,
        Fut: Future<Output = Result<T, RpcError>>,
    {
        let mut order: Vec<String> = Vec::new();
        if let Some(current) = self.current_base_url() {
            order.push(current);
        }
        for endpoint in &self.endpoints {
            if !order.contains(&endpoint.base_url) {
                order.push(endpoint.base_url.clone());
            }
        }
        if order.is_empty() {
            return Err(CoreError::NoEndpoint);
        }

        let mut last: Option<RpcError> = None;
        for base_url in order {
            match run(self.transport.clone(), base_url.clone()).await {
                Ok(value) => {
                    self.current.store(Arc::new(Some(base_url)));
                    return Ok(value);
                }
                Err(err) if err.is_retryable() => last = Some(err),
                Err(err) => return Err(err.into()),
            }
        }
        Err(last.map(Into::into).unwrap_or(CoreError::NoEndpoint))
    }

    pub async fn login(
        &self,
        username: String,
        password: String,
        machine: Option<Arc<MachineFacts>>,
        launcher_version: String,
    ) -> Result<LoginResponse, CoreError> {
        self.call(|transport, base_url| {
            let username = username.clone();
            let password = password.clone();
            let machine = machine.clone();
            let launcher_version = launcher_version.clone();
            async move {
                let client = LauncherClient::new(&transport, &base_url);
                let mut report = None;
                if let Some(facts) = &machine {
                    let challenge = client.challenge().await?;
                    if !challenge.nonce.is_empty() {
                        report = Some(facts.report(challenge.nonce, &launcher_version));
                    }
                }
                client.login(username, password, report).await
            }
        })
        .await
    }

    pub async fn refresh(&self, refresh: String) -> Result<Tokens, CoreError> {
        self.call(|transport, base_url| {
            let refresh = refresh.clone();
            async move {
                LauncherClient::new(&transport, &base_url)
                    .refresh(refresh)
                    .await
            }
        })
        .await
    }

    pub async fn report_machine(
        &self,
        machine: Arc<MachineFacts>,
        launcher_version: String,
    ) -> Result<Option<MachineVerdict>, CoreError> {
        self.call(|transport, base_url| {
            let machine = machine.clone();
            let launcher_version = launcher_version.clone();
            async move {
                let client = LauncherClient::new(&transport, &base_url);
                let challenge = client.challenge().await?;
                if challenge.nonce.is_empty() {
                    return Ok(None);
                }
                let response = client
                    .report_machine(machine.report(challenge.nonce, &launcher_version))
                    .await?;
                Ok(response.verdict)
            }
        })
        .await
    }

    pub async fn news(&self) -> Result<Vec<NewsItem>, CoreError> {
        self.call(|transport, base_url| async move {
            LauncherClient::new(&transport, &base_url).news().await
        })
        .await
    }

    pub async fn list_profiles(&self) -> Result<Vec<ProfileSummary>, CoreError> {
        self.call(|transport, base_url| async move {
            LauncherClient::new(&transport, &base_url)
                .list_profiles()
                .await
        })
        .await
    }

    pub async fn check_update(
        &self,
        current_version: String,
    ) -> Result<crate::proto::api::v1::CheckUpdateResponse, CoreError> {
        self.call(|transport, base_url| {
            let version = current_version.clone();
            async move {
                LauncherClient::new(&transport, &base_url)
                    .check_update(version)
                    .await
            }
        })
        .await
    }

    pub async fn get_manifest(&self, profile: String) -> Result<GetManifestResponse, CoreError> {
        self.call(|transport, base_url| {
            let profile = profile.clone();
            async move {
                LauncherClient::new(&transport, &base_url)
                    .get_manifest(profile)
                    .await
            }
        })
        .await
    }
}
