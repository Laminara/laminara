use std::future::Future;
use std::sync::OnceLock;

use tokio::runtime::Runtime;

static RUNTIME: OnceLock<Runtime> = OnceLock::new();

pub fn get() -> &'static Runtime {
    RUNTIME.get_or_init(|| {
        tokio::runtime::Builder::new_multi_thread()
            .worker_threads(2)
            .enable_all()
            .thread_name("laminara-core")
            .build()
            .expect("tokio runtime")
    })
}

pub async fn on_core<F>(work: F) -> F::Output
where
    F: Future + Send + 'static,
    F::Output: Send + 'static,
{
    match get().spawn(work).await {
        Ok(output) => output,
        Err(error) => std::panic::resume_unwind(error.into_panic()),
    }
}
