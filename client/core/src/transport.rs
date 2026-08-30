use std::sync::{Arc, RwLock};

use prost::Message;
use serde::Deserialize;

use crate::error::RpcError;

#[derive(Clone)]
pub struct Transport {
    http: reqwest::Client,
    access_token: Arc<RwLock<Option<String>>>,
    machine_ticket: Arc<RwLock<Option<String>>>,
}

#[derive(Deserialize)]
struct ConnectError {
    code: String,
    #[serde(default)]
    message: Option<String>,
    #[serde(default)]
    details: Option<Vec<ConnectErrorDetail>>,
}

#[derive(Deserialize)]
struct ConnectErrorDetail {
    #[serde(rename = "type")]
    kind: String,
}

const SECOND_FACTOR_DETAIL: &str = "laminara.api.v1.TwoFactorRequired";

pub fn default_http_client() -> reqwest::Client {
    reqwest::Client::builder()
        .http1_only()
        .use_rustls_tls()
        .redirect(reqwest::redirect::Policy::limited(5))
        .build()
        .expect("build http client")
}

impl Transport {
    pub fn new(http: reqwest::Client) -> Self {
        Self {
            http,
            access_token: Arc::new(RwLock::new(None)),
            machine_ticket: Arc::new(RwLock::new(None)),
        }
    }

    pub fn set_access_token(&self, token: Option<String>) {
        *self.access_token.write().unwrap() = token;
    }

    pub fn set_machine_ticket(&self, ticket: Option<String>) {
        *self.machine_ticket.write().unwrap() = ticket;
    }

    pub fn machine_ticket(&self) -> Option<String> {
        self.machine_ticket.read().unwrap().clone()
    }

    pub fn authorize(&self, request: reqwest::RequestBuilder) -> reqwest::RequestBuilder {
        match self.access_token.read().unwrap().as_deref() {
            Some(token) => request.bearer_auth(token),
            None => request,
        }
    }

    pub fn client(&self) -> &reqwest::Client {
        &self.http
    }

    pub async fn unary<Req, Resp>(
        &self,
        base_url: &str,
        method_path: &str,
        request: &Req,
    ) -> Result<Resp, RpcError>
    where
        Req: Message,
        Resp: Message + Default,
    {
        let url = format!("{}/{}", base_url.trim_end_matches('/'), method_path);
        let body = request.encode_to_vec();

        let response = self
            .authorize(self.http.post(&url))
            .header("Content-Type", "application/proto")
            .header("Connect-Protocol-Version", "1")
            .body(body)
            .send()
            .await
            .map_err(|e| RpcError::PreSend(e.to_string()))?;

        let status = response.status();
        let bytes = response
            .bytes()
            .await
            .map_err(|e| RpcError::PostSend(e.to_string()))?;

        if status.is_success() {
            return Resp::decode(bytes).map_err(|e| RpcError::PostSend(format!("decode: {e}")));
        }
        match serde_json::from_slice::<ConnectError>(&bytes) {
            Ok(err) => Err(RpcError::App {
                code: self::second_factor_kind(&err).unwrap_or(err.code),
                message: err.message.unwrap_or_default(),
            }),
            Err(_) => Err(RpcError::PostSend(format!("http {status}"))),
        }
    }
}

fn second_factor_kind(err: &ConnectError) -> Option<String> {
    let demanded = err
        .details
        .iter()
        .flatten()
        .any(|detail| detail.kind.ends_with(SECOND_FACTOR_DETAIL));
    demanded.then(|| crate::error::SECOND_FACTOR_CODE.to_string())
}

impl Default for Transport {
    fn default() -> Self {
        Self::new(default_http_client())
    }
}
