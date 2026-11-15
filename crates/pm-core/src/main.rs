//! The Rust Portmaster core binary.
//!
//! Starts the database system, the configuration system and the HTTP API, and
//! keeps running until it receives a shutdown signal. The restart exit code is
//! [`pm_core::cli::RESTART_EXIT_CODE`], like in the Go core.

use std::process::ExitCode;

use pm_core::cli;
use pm_core::service::Service;

fn main() -> ExitCode {
    let options = match cli::parse(std::env::args().skip(1)) {
        Ok(options) => options,
        Err(error) => {
            eprintln!("{error}");
            eprintln!();
            eprintln!("{}", cli::help());
            return ExitCode::FAILURE;
        }
    };

    if options.print_version {
        println!("{}", cli::version());
        return ExitCode::SUCCESS;
    }
    if options.help {
        println!("{}", cli::help());
        return ExitCode::SUCCESS;
    }

    let runtime = match tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
    {
        Ok(runtime) => runtime,
        Err(error) => {
            eprintln!("failed to start runtime: {error}");
            return ExitCode::FAILURE;
        }
    };

    runtime.block_on(async {
        // The log directory may not exist yet; create it before logging starts.
        if !options.log_to_stdout {
            let _ = std::fs::create_dir_all(&options.log_dir);
        }
        pm_core::logging::init(&options);
        tracing::info!(version = %cli::version(), "starting core");

        let service = match Service::start(&options).await {
            Ok(service) => std::sync::Arc::new(service),
            Err(error) => {
                tracing::error!(%error, "failed to start core");
                eprintln!("failed to start core: {error}");
                return ExitCode::FAILURE;
            }
        };

        tracing::info!(
            address = %service.local_addr(),
            "core is running"
        );

        match service.run().await {
            Ok(()) => ExitCode::SUCCESS,
            Err(error) => {
                tracing::error!(%error, "core failed");
                eprintln!("core failed: {error}");
                ExitCode::FAILURE
            }
        }
    })
}
