use thiserror::Error;

#[derive(Debug, Error)]
pub enum CoreError {
    #[error("no endpoint available")]
    NoEndpoint,
    #[error("{message}")]
    App { code: String, message: String },
    #[error("transport: {0}")]
    Transport(String),
    #[error("manifest signature is not trusted")]
    Untrusted,
    #[error("config: {0}")]
    Config(String),
    #[error("sync: {0}")]
    Sync(String),
    #[error("launch: {0}")]
    Launch(String),
    #[error("io: {0}")]
    Io(String),
}

impl From<std::io::Error> for CoreError {
    fn from(err: std::io::Error) -> Self {
        CoreError::Io(err.to_string())
    }
}

impl From<RpcError> for CoreError {
    fn from(err: RpcError) -> Self {
        match err {
            RpcError::PreSend(m) | RpcError::PostSend(m) => CoreError::Transport(m),
            RpcError::App { code, message } => CoreError::App { code, message },
        }
    }
}

#[derive(Debug, Clone)]
pub enum RpcError {
    PreSend(String),
    PostSend(String),
    App { code: String, message: String },
}

impl RpcError {
    pub fn is_retryable(&self) -> bool {
        matches!(self, RpcError::PreSend(_))
    }
}

impl std::fmt::Display for RpcError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            RpcError::PreSend(m) => write!(f, "pre-send: {m}"),
            RpcError::PostSend(m) => write!(f, "post-send: {m}"),
            RpcError::App { code, message } => write!(f, "[{code}] {message}"),
        }
    }
}
