use std::{fs::File, io::Write, process::Command};

use chrono::Local;
use handlebars::Handlebars;
use serde_json::json;
use zip::{write::FileOptions, ZipWriter};

static VERSION: [u8; 4] = include!("../../kext_interface/version.txt");
static LIB_PATH: &'static str = "./build/x86_64-pc-windows-msvc/release/driver.lib";

fn main() {
    build_driver();
    println!(
        "Building kext v{}-{}-{} #{}",
        VERSION[0], VERSION[1], VERSION[2], VERSION[3]
    );

    // Create Zip that will hold all the release files and scripts.
    let file = File::create(format!(
        "kext_release_v{}-{}-{}.zip",
        VERSION[0], VERSION[1], VERSION[2]
    ))
    .unwrap();
    let mut zip = zip::ZipWriter::new(file);

    let version_file = format!(
        "portmaster-kext_v{}-{}-{}",
        VERSION[0], VERSION[1], VERSION[2]
    );

    // Write files to zip
    zip.add_directory("cab", FileOptions::default()).unwrap();
    // Write driver.lib
    write_lib_file_zip(&mut zip);
    // Write ddf file
    write_to_zip(
        &mut zip,
        &format!("{}.ddf", version_file),
        get_ddf_content(),
    );
    // Write build cab script
    write_to_zip(&mut zip, "build_cab.ps1", get_build_cab_script_content());

    // Write inf file
    write_to_zip(
        &mut zip,
        &format!("cab/{}.inf", version_file),
        get_inf_content(),
    );

    zip.finish().unwrap();
}

fn version_str() -> String {
    return format!(
        "{}.{}.{}.{}",
        VERSION[0], VERSION[1], VERSION[2], VERSION[3]
    );
}

fn build_driver() {
    let output = Command::new("cargo")
        .current_dir("../driver")
        .arg("build")
        .arg("--release")
        .args(["--target", "x86_64-pc-windows-msvc"])
        .args(["--target-dir", "../release/build"])
        .output()
        .unwrap();
    println!("{}", String::from_utf8(output.stderr).unwrap());
}

fn get_inf_content() -> String {
    let reg = Handlebars::new();
    let today = Local::now();
    reg.render_template(
        include_str!("../templates/PortmasterKext64.inf"),
        &json!({"date": today.format("%m/%d/%Y").to_string(), "version": version_str()}),
    )
    .unwrap()
}

fn get_ddf_content() -> String {
    let reg = Handlebars::new();
    let version_file = format!(
        "portmaster-kext_v{}-{}-{}",
        VERSION[0], VERSION[1], VERSION[2]
    );
    reg.render_template(
        include_str!("../templates/PortmasterKext.ddf"),
        &json!({"version_file": version_file}),
    )
    .unwrap()
}

fn get_build_cab_script_content() -> String {
    let reg = Handlebars::new();
    let version_file = format!(
        "portmaster-kext_v{}-{}-{}",
        VERSION[0], VERSION[1], VERSION[2]
    );

    reg
        .render_template(
            include_str!("../templates/build_cab.ps1"),
            &json!({"sys_file": format!("{}.sys", version_file), "pdb_file": format!("{}.pdb", version_file), "lib_file": "driver.lib", "version_file": &version_file }),
        )
        .unwrap()
}

fn write_to_zip(zip: &mut ZipWriter<File>, filename: &str, content: String) {
    zip.start_file(filename, FileOptions::default()).unwrap();
    zip.write(&content.into_bytes()).unwrap();
}

fn write_lib_file_zip(zip: &mut ZipWriter<File>) {
    zip.start_file("driver.lib", FileOptions::default())
        .unwrap();
    let mut driver_file = File::open(LIB_PATH).unwrap();
    std::io::copy(&mut driver_file, zip).unwrap();
}
