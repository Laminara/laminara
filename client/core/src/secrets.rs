use std::any::Any;

use keyring::Entry;

pub fn read_password(entry: Entry) -> Result<Option<String>, String> {
    match entry.get_password() {
        Ok(value) => Ok(Some(value)),
        Err(keyring::Error::NoEntry) => Ok(None),
        Err(e) => Err(e.to_string()),
    }
}

pub fn delete_password(entry: Entry) -> Result<(), String> {
    match entry.delete_credential() {
        Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
        Err(e) => Err(e.to_string()),
    }
}

pub fn blocking<T, F>(work: F) -> Result<T, String>
where
    F: FnOnce() -> Result<T, String> + Send + 'static,
    T: Send + 'static,
{
    let handle = std::thread::Builder::new()
        .name("keyring".into())
        .spawn(work)
        .map_err(|e| e.to_string())?;
    match handle.join() {
        Ok(result) => result,
        Err(panic) => Err(panic_message(panic)),
    }
}

fn panic_message(panic: Box<dyn Any + Send>) -> String {
    if let Some(message) = panic.downcast_ref::<String>() {
        return message.clone();
    }
    if let Some(message) = panic.downcast_ref::<&'static str>() {
        return (*message).into();
    }
    "keyring worker failed".into()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn blocking_returns_the_worker_result() {
        assert!(matches!(blocking(|| Ok::<u8, String>(7)), Ok(7)));
        assert!(matches!(
            blocking(|| Err::<u8, String>("denied".into())),
            Err(message) if message == "denied"
        ));
    }

    #[test]
    fn blocking_survives_a_panicking_worker() {
        let result: Result<u8, String> = blocking(|| panic!("keyring exploded"));
        assert!(matches!(result, Err(message) if message.contains("keyring exploded")));
    }
}
