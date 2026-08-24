pub fn blocking<T, F>(work: F) -> T
where
    F: FnOnce() -> T + Send,
    T: Send,
{
    std::thread::scope(|scope| scope.spawn(work).join().expect("keyring thread"))
}
