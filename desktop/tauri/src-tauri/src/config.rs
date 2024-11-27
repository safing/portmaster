use std::fs;

use log::{debug, error};
use serde::{Deserialize, Serialize};
use tauri::{AppHandle, Manager};

#[derive(Serialize, Deserialize)]
pub enum Theme {
    Light,
    Dark,
    System,
}

#[derive(Serialize, Deserialize)]
pub struct Config {
    pub theme: Theme,
}

const CONFIG_FILE_NAME: &'static str = "config.json";

pub fn save(app: &AppHandle, config: Config) -> tauri::Result<()> {
    let config_dir = app.path().app_config_dir()?;

    let config_path = config_dir.join(CONFIG_FILE_NAME);
    debug!("saving config file: {:?}", config_path);
    let json = serde_json::to_string_pretty(&config)?;
    fs::write(config_path, json)?;
    Ok(())
}

pub fn load(app: &AppHandle) -> tauri::Result<Config> {
    let config_dir = app.path().app_config_dir()?;

    let config_path = config_dir.join(CONFIG_FILE_NAME);
    if let Ok(json) = fs::read_to_string(config_path) {
        if let Ok(config) = serde_json::from_str(&json) {
            return Ok(config);
        }
    }

    error!("failed to load config file returning default config");
    Ok(Config {
        theme: Theme::System,
    })
}
