# rust-dark-light

Rust crate to detect if dark mode or light mode is enabled. Supports macOS, Windows, Linux, BSDs, and WASM. On Linux and BSDs, first the XDG Desktop Portal dbus API is checked for the `color-scheme` preference, which works in Flatpak sandboxes without needing filesystem access. If that does not work, fallback methods are used for KDE, GNOME, Cinnamon, MATE, XFCE, and Unity.

[API Documentation](https://docs.rs/dark-light/)

## Usage

```rust
fn main() {
    let mode = dark_light::detect();

    match mode {
        // Dark mode
        dark_light::Mode::Dark => {},
        // Light mode
        dark_light::Mode::Light => {},
        // Unspecified
        dark_light::Mode::Default => {},
    }
}
```

## Example

```
cargo run --example detect
```

## License

Licensed under either of

 * Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE) or http://www.apache.org/licenses/LICENSE-2.0)
 * MIT license ([LICENSE-MIT](LICENSE-MIT) or http://opensource.org/licenses/MIT)

at your option.


