# Update Tauri guide

Check latest versions of tauri packages and update them accordingly (https://crates.io/)  
Cargo.toml:  
```toml
[build-dependencies]
tauri-build = { version = "x.x.x-beta.19", features = [] } # Update to latest

[dependencies]
# Tauri
tauri = { version = "x.x.x-beta.24", features = ["tray-icon", "image-png", "config-json5", "devtools"] } # Update to latest
tauri-plugin-shell = "x.x.x-beta"
tauri-plugin-dialog = "x.x.x-beta"
tauri-plugin-clipboard-manager = "x.x.x-beta"
tauri-plugin-os = "x.x.x-beta"
tauri-plugin-single-instance = "x.x.x-beta"
tauri-plugin-cli = "x.x.x-beta"
tauri-plugin-notification = "x.x.x-beta"
tauri-plugin-log = "x.x.x-beta"
tauri-plugin-window-state = "x.x.x-beta"

tauri-cli = "x.x.x-beta.21" # Update to latest
```

Run:
```sh
cargo update
```

> Make sure to update the npm tauri plugin dependencies to have the same version as the rust plugins. (desktop/angular)

## Update WIX installer template

> If the migration functionality is not needed anymore remove the template, this will cause tauri to use its default template and not call the migration script.

1. Get the latest [main.wxs](https://github.com/tauri-apps/tauri/blob/dev/tooling/bundler/src/bundle/windows/templates/main.wxs) template from the repository.
2. Replace the contents of `templates/wix/main_original.wxs` with the repository version. (The file is kept only for reference)
3. Replace the contents of `templates/wix/main.wsx` and add the fallowing lines at the end of the file, inside the `Product` tag. 
```xml
    <!-- Service fragments -->
    <CustomActionRef Id='MigrationPropertySet' />
    <CustomActionRef Id='Migration' />
    <!-- Uncommenting the next line will cause the installer to check if the old service is running and fail. Without it, it will automatically stop and remove the old service without notifying the user. -->
    <!-- <CustomActionRef Id='CheckServiceStatus' /> -->
    <!-- End Service fragments -->
```
