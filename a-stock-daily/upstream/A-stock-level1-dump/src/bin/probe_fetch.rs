use anyhow::Result;
use stock_fetcher::{config, fetcher};

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter("info")
        .init();

    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 {
        eprintln!("usage: {} CODE YYYYMMDD", args[0]);
        std::process::exit(2);
    }

    let code = &args[1];
    let date: u32 = args[2].parse()?;
    let cfg = config::Config::load("config.toml")?;
    let client = fetcher::HighPerfTcpClient::new(
        cfg.server.host,
        cfg.server.port,
        cfg.server.timeout_secs,
        2,
    )?;

    let data = client.fetch(code, date).await?;
    let min_time = data.iter().map(|row| row.time_sec).min();
    let max_time = data.iter().map(|row| row.time_sec).max();

    println!(
        "code={} date={} rows={} min_time={:?} max_time={:?}",
        code,
        date,
        data.len(),
        min_time,
        max_time
    );

    Ok(())
}
