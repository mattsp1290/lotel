use anyhow::{Result, bail};
use chrono::{Duration, NaiveDateTime, Utc};

/// Parse a time string as RFC 3339 or a relative duration (e.g. "1h", "24h", "7d").
pub fn parse_time(s: &str) -> Result<NaiveDateTime> {
    // Try RFC 3339 first.
    if let Ok(dt) = chrono::DateTime::parse_from_rfc3339(s) {
        return Ok(dt.naive_utc());
    }
    // Try relative duration.
    let dur = parse_duration(s)?;
    Ok(Utc::now().naive_utc() - dur)
}

/// Parse a duration string. Supports "Nd" for days, and standard h/m/s suffixes.
pub fn parse_duration(s: &str) -> Result<Duration> {
    let s = s.trim();
    if s.is_empty() {
        bail!("empty duration string");
    }

    let (num_str, suffix) = s.split_at(s.len() - 1);
    let value: i64 = num_str
        .parse()
        .map_err(|_| anyhow::anyhow!("cannot parse {s:?} as duration"))?;

    match suffix {
        "d" => Ok(Duration::days(value)),
        "h" => Ok(Duration::hours(value)),
        "m" => Ok(Duration::minutes(value)),
        "s" => Ok(Duration::seconds(value)),
        _ => bail!("cannot parse {s:?} as duration (unknown suffix {suffix:?})"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_duration_days() {
        let d = parse_duration("7d").unwrap();
        assert_eq!(d, Duration::days(7));
    }

    #[test]
    fn parse_duration_hours() {
        let d = parse_duration("24h").unwrap();
        assert_eq!(d, Duration::hours(24));
    }

    #[test]
    fn parse_duration_minutes() {
        let d = parse_duration("30m").unwrap();
        assert_eq!(d, Duration::minutes(30));
    }

    #[test]
    fn parse_duration_seconds() {
        let d = parse_duration("60s").unwrap();
        assert_eq!(d, Duration::seconds(60));
    }

    #[test]
    fn parse_time_rfc3339() {
        let t = parse_time("2024-01-15T10:30:00Z").unwrap();
        assert_eq!(t.to_string(), "2024-01-15 10:30:00");
    }

    #[test]
    fn parse_time_relative() {
        let before = Utc::now().naive_utc() - Duration::hours(1) - Duration::seconds(5);
        let t = parse_time("1h").unwrap();
        let after = Utc::now().naive_utc() - Duration::hours(1) + Duration::seconds(5);
        assert!(t > before && t < after);
    }
}
