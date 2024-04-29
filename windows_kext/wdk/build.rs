#[cfg(target_arch = "x86_64")]
fn main() {
    // C Helper
    println!("cargo:rerun-if-changed=../c_helper/x64/c_helper.lib");
    println!("cargo:rustc-link-search=native=../c_helper/x64");
}

#[cfg(target_arch = "aarch64")]
fn main() {
    // C Helper
    println!("cargo:rerun-if-changed=../c_helper/ARM64/c_helper.lib");
    println!("cargo:rustc-link-search=native=../c_helper/ARM64");
}
