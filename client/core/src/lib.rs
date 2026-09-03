pub mod proto;

pub mod account;
pub mod config;
pub mod endpoint;
pub mod error;
pub mod features;
pub mod launch;
pub mod machine;
pub mod manifest;
pub mod paths;
pub mod platform;
pub mod privatefile;
pub mod process;
pub mod rpc;
pub mod secrets;
pub mod slp;
pub mod state;
pub mod sync;
pub mod transport;
pub mod update;

pub const KEYRING_SERVICE: &str = "dev.mrleonardos.laminara";

pub use error::{CoreError, RpcError};
pub use state::{Account, Core, LoginResult};
