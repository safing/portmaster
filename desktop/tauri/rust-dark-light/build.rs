fn main() {
    if let Ok("apple") = std::env::var("CARGO_CFG_TARGET_VENDOR").as_deref() {
        println!("cargo:rustc-link-lib=framework=AppKit");
    }
}
