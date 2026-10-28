{
  description = "Portmaster development environment (Go core, Angular UI, Tauri app)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = {
    self,
    nixpkgs,
  }: let
    # Portmaster only builds for Linux and Windows. The Windows artifacts
    # (windows_kext, windows_core_dll) are cross-compiled from Linux, so the
    # dev shells target Linux hosts only.
    systems = ["x86_64-linux" "aarch64-linux"];
    forAllSystems = fun:
      nixpkgs.lib.genAttrs systems (system: fun nixpkgs.legacyPackages.${system});
  in {
    devShells = forAllSystems (pkgs: let
      # Native libraries the Tauri app links against. Kept in one list so
      # they can be fed to both buildInputs and LD_LIBRARY_PATH.
      tauriLibs = with pkgs; [
        at-spi2-atk
        atkmm
        cairo
        dbus
        gdk-pixbuf
        glib
        gtk3
        harfbuzz
        libayatana-appindicator
        librsvg
        libsoup_3
        openssl
        pango
        webkitgtk_4_1
      ];

      goTools = with pkgs; [
        go # 1.26.x, matches the `go`/`toolchain` directives in go.mod
        delve
        golangci-lint
        gopls
      ];

      # Angular 16 predates the current Node LTS; 22 is the newest release
      # its CLI engine range still accepts.
      webTools = [pkgs.nodejs_22];

      rustTools = with pkgs; [
        cargo
        cargo-tauri
        clippy
        rust-analyzer
        rustc
        rustfmt
      ];

      # Opt-in launcher for the Tauri app. LD_LIBRARY_PATH must NOT be
      # exported shell-wide: unrelated programs started from the shell
      # (editors, browsers) inherit it too, which forces this nixpkgs'
      # glib/pixman/sqlite in front of their own and breaks them with
      # `GLIBC_2.xx not found` when they are linked against another glibc.
      # Scope it to the command that actually needs it instead.
      withTauriLibs = pkgs.writeShellScriptBin "with-tauri-libs" ''
        # libayatana-appindicator is dlopen()ed by the tray code, so the
        # rpath the linker records is not enough on its own.
        export LD_LIBRARY_PATH="${pkgs.lib.makeLibraryPath tauriLibs}''${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
        export GIO_EXTRA_MODULES="${pkgs.glib-networking}/lib/gio/modules''${GIO_EXTRA_MODULES:+:$GIO_EXTRA_MODULES}"

        # GTK dialogs (rfd) and the tray icon need the schemas and icon
        # themes to be discoverable outside of a wrapped derivation.
        export XDG_DATA_DIRS="${pkgs.gsettings-desktop-schemas}/share/gsettings-schemas/${pkgs.gsettings-desktop-schemas.name}:${pkgs.gtk3}/share/gsettings-schemas/${pkgs.gtk3.name}:${pkgs.shared-mime-info}/share:${pkgs.adwaita-icon-theme}/share''${XDG_DATA_DIRS:+:$XDG_DATA_DIRS}"

        # WebKitGTK Hardware Acceleration & Rendering Workarounds
        # -----------------------------------------------------------------
        # Uncomment these if you experience blank, black, or flickering windows
        # (frequently encountered on NVIDIA, hybrid graphics, or certain
        # Wayland/DMABUF setups). Leave them commented out to enjoy full
        # GPU-accelerated rendering if your graphics stack supports it.
        #
        # - WEBKIT_DISABLE_COMPOSITING_MODE=1: Forces software-rendered fallback.
        # - WEBKIT_DISABLE_DMABUF_RENDERER=1: Disables Direct Rendering Manager
        #   Buffer sharing (often fixes blank windows on modern Wayland/EGL).
        # export WEBKIT_DISABLE_COMPOSITING_MODE=1
        # export WEBKIT_DISABLE_DMABUF_RENDERER=1

        exec "$@"
      '';

      buildTools = with pkgs; [
        gcc
        pkg-config
        patchelf
        file
        gobject-introspection
      ];
    in {
      default = pkgs.mkShell {
        name = "portmaster";

        packages =
          goTools
          ++ webTools
          ++ rustTools
          ++ buildTools
          ++ tauriLibs
          ++ [withTauriLibs];

        # nixpkgs' Go is newer than the `toolchain` directive in go.mod, so
        # pin to it instead of letting Go fetch its own toolchain tarball.
        GOTOOLCHAIN = "local";

        RUST_SRC_PATH = "${pkgs.rustPlatform.rustLibSrc}";

        shellHook = ''
          echo "portmaster dev shell"
          echo "  go      $(go version | cut -d' ' -f3)"
          echo "  node    $(node --version)"
          echo "  rustc   $(rustc --version | cut -d' ' -f2)"
          echo
          echo "  run the desktop app with:  with-tauri-libs cargo tauri dev"
        '';
      };
    });

    formatter = forAllSystems (pkgs: pkgs.nixfmt);
  };
}
