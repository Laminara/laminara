use std::time::Duration;

pub fn pause(attempt: u32, first: Duration, longest: Duration) -> Duration {
    let ceiling = first
        .saturating_mul(1u32 << attempt.saturating_sub(1).min(16))
        .min(longest);
    let half = ceiling / 2;
    half + half.mul_f64(rand::random::<f64>())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_pause_lands_between_half_and_the_whole_step() {
        let first = Duration::from_secs(1);
        let longest = Duration::from_secs(15);
        for (attempt, step) in [(1, 1), (2, 2), (3, 4), (4, 8), (5, 15), (9, 15)] {
            let step = Duration::from_secs(step);
            for _ in 0..50 {
                let pause = pause(attempt, first, longest);
                assert!(pause >= step / 2 && pause <= step, "attempt {attempt}: {pause:?}");
            }
        }
    }

    #[test]
    fn pauses_spread_so_that_parallel_retries_do_not_march_together() {
        let pauses: std::collections::HashSet<_> = (0..20)
            .map(|_| pause(3, Duration::from_secs(1), Duration::from_secs(15)).as_millis())
            .collect();
        assert!(pauses.len() > 1);
    }
}
