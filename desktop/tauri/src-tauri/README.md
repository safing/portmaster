# Update Tauri guide

Check latest versions of tauri packages and update them accordingly:
```toml
[build-dependencies]
tauri-build = { version = "2.0.0-beta.19", features = [] } # Update to latest

[dependencies]
# Tauri
tauri = { version = "2.0.0-beta.24", features = ["tray-icon", "image-png", "config-json5", "devtools"] } # Update to latest
tauri-plugin-shell = "2.0.0-beta"
tauri-plugin-dialog = "2.0.0-beta"
tauri-plugin-clipboard-manager = "2.0.0-beta"
tauri-plugin-os = "2.0.0-beta"
tauri-plugin-single-instance = "2.0.0-beta"
tauri-plugin-cli = "2.0.0-beta"
tauri-plugin-notification = "2.0.0-beta"
tauri-plugin-log = "2.0.0-beta"
tauri-plugin-window-state = "2.0.0-beta"

tauri-cli = "2.0.0-beta.21" # Update to latest
```

> The plugins will be auto updated based on tauri version.

Run:
```sh
cargo update
```

Update WIX installer template:
1. Get the latests [main.wxs](https://github.com/tauri-apps/tauri/blob/dev/tooling/bundler/src/bundle/windows/templates/main.wxs) template from the repository.
2. Replace the contents of `templates/main_original.wxs` with the repository version.
3. Replace the contents of `templates/main.wsx` and add the fallowing lines at the end of the file, inside the `Product` tag. 
```xml
    <CustomActionRef Id='InstallPortmasterService' />
    <CustomActionRef Id='StopPortmasterService' />
    <CustomActionRef Id='DeletePortmasterService' />
```
