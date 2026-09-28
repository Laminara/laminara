use thiserror::Error;

pub const SECOND_FACTOR_CODE: &str = "second_factor_required";

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
    #[error("cancelled")]
    Cancelled,
}

pub fn describe(error: &(dyn std::error::Error + 'static)) -> String {
    let mut text = error.to_string();
    let mut source = error.source();
    while let Some(cause) = source {
        let part = cause.to_string();
        if !part.is_empty() && !text.contains(&part) {
            text.push_str(": ");
            text.push_str(&part);
        }
        source = cause.source();
    }
    text
}

impl CoreError {
    pub fn server_unreachable(&self) -> bool {
        matches!(self, CoreError::Transport(_) | CoreError::NoEndpoint)
    }
}

impl From<std::io::Error> for CoreError {
    fn from(err: std::io::Error) -> Self {
        CoreError::Io(err.to_string())
    }
}

impl From<reqwest::Error> for CoreError {
    fn from(err: reqwest::Error) -> Self {
        CoreError::Transport(describe(&err))
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

#[cfg(test)]
mod tests {
    use super::describe;

    #[derive(Debug)]
    struct Layer(&'static str, Option<Box<Layer>>);

    impl std::fmt::Display for Layer {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            f.write_str(self.0)
        }
    }

    impl std::error::Error for Layer {
        fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
            self.1.as_deref().map(|layer| layer as _)
        }
    }

    #[test]
    fn the_whole_chain_of_causes_reaches_the_log() {
        let error = Layer(
            "error sending request for url (https://play.example/objects/a)",
            Some(Box::new(Layer(
                "client error (Connect)",
                Some(Box::new(Layer(
                    "tcp connect error: timed out (os error 10060)",
                    None,
                ))),
            ))),
        );
        assert_eq!(
            describe(&error),
            "error sending request for url (https://play.example/objects/a): client error (Connect): tcp connect error: timed out (os error 10060)"
        );
    }

    #[test]
    fn a_cause_already_quoted_in_the_message_is_not_repeated() {
        let error = Layer(
            "stream: connection reset",
            Some(Box::new(Layer("connection reset", None))),
        );
        assert_eq!(describe(&error), "stream: connection reset");
    }
}
