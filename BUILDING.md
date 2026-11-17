# portmasterpro — Build & Test Guide

Diese Anleitung beschreibt wie du portmasterpro lokal kompilierst, testest
und die `no-spn-no-telemetry`-Änderungen verifizierst.

---

## Voraussetzungen

| Tool | Mindestversion | Installieren |
|---|---|---|
| Go | 1.26 (siehe `go.mod`, `toolchain go1.26.3` wird bei Bedarf automatisch geladen) | https://go.dev/dl/ |
| Git | beliebig | https://git-scm.com |
| Node.js | 20 LTS | https://nodejs.org (nur für Desktop-Tests) |
| pnpm / npm | beliebig | `npm i -g pnpm` (nur für Desktop-Tests) |
| gcc / build-essential | beliebig | `sudo apt install build-essential` (Linux) |

> **Windows:** Nutze WSL2 (Ubuntu 22.04+) oder installiere
> [TDM-GCC](https://jmeubank.github.io/tdm-gcc/) für cgo-Support.

---

## 1 · Repository klonen

```bash
git clone https://github.com/0n113/portmasterpro.git
cd portmasterpro

# Branch mit den Telemetrie-/SPN-Bereinigungen auschecken
git checkout no-spn-no-telemetry
```

---

## 2 · Go-Abhängigkeiten laden & aufräumen

```bash
# Abhängigkeiten herunterladen
go mod download

# Nicht mehr benötigte Einträge aus go.mod / go.sum entfernen
# (nach dem Entfernen der SPN-Pakete wichtig)
go mod tidy
```

> `go mod tidy` aktualisiert `go.sum` automatisch.
> Veränderte Dateien danach committen:
> ```bash
> git add go.mod go.sum
> git commit -m "chore: go mod tidy after SPN removal"
> ```

---

## 3 · Go-Code kompilieren

### Nur prüfen ob alles kompiliert (kein Binary)

```bash
go build ./...
```

Erwartete Ausgabe: keine Fehler, kein Output.

### Binary bauen (Linux)

```bash
mkdir -p build
go build -o build/portmasterpro ./cmds/portmaster-core/
```

### Binary bauen (Windows, in WSL2)

```bash
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 \
  CC=x86_64-w64-mingw32-gcc \
  go build -o build/portmasterpro.exe ./cmds/portmaster-core/
```

---

## 4 · Go-Tests ausführen

### Alle Tests (wie in der CI)

```bash
go test -short -count=1 ./...
```

`-short` entspricht der Upstream-Konvention (`Earthfile`, Target `+go-test`):
"lange" Tests brauchen ein vollständiges Desktop-System.

**Bekannte umgebungsabhängige Fehler** (treten identisch im unveränderten
Upstream `development` auf, keine Regression dieses Branches):

| Paket / Test | Ursache |
|---|---|
| `service/compat` · `TestIPTablesChains` | benötigt `iptables` im PATH |
| `service/netenv` · `TestCheckOnlineStatus` | benötigt Internetzugang |
| `service/resolver` · `TestResolveIPAndValidate` | benötigt Internetzugang (Reverse-DNS) |
| `service/profile/binmeta` · `TestFindIcon` | benötigt Desktop-Icons (evolution, nextcloud) |
| `service/profile/endpoints` · `TestEndpointMatching` | benötigt GeoIP-Datenbank |
| `spn/crew` · `TestConnectOp` | schlägt auch upstream fehl (EOF) |

`spn/docks · TestExpansion` wird bewusst per `t.Skip` übersprungen: Der Test
authentifiziert sich mit SPN-Zugangstokens, die in portmasterpro nicht mehr
existieren.

### Nur die Telemetrie-/SPN-Stub-Tests

```bash
# service/sync — Stub startet/stoppt ohne Fehler, kein Manager
go test -v ./service/sync/...

# spn/access — kein Login, alle Features offen, kein Manager
go test -v ./spn/access/...

# service/netquery — Endpoints ungated, kein SPN-Import
go test -v ./service/netquery/...
```

### Erwartete Ausgabe (Beispiel `spn/access`)

```
=== RUN   TestAccessStubNoOp
--- PASS: TestAccessStubNoOp (0.00s)
=== RUN   TestAccessStubNotLoggedIn
--- PASS: TestAccessStubNotLoggedIn (0.00s)
=== RUN   TestAccessStubAllFeaturesUnlocked
--- PASS: TestAccessStubAllFeaturesUnlocked (0.00s)
=== RUN   TestAccessStubManagerIsNil
--- PASS: TestAccessStubManagerIsNil (0.00s)
PASS
ok  	github.com/safing/portmaster/spn/access
```

### Mit Race-Detector (empfohlen vor einem Merge)

```bash
go test -race ./service/sync/... ./spn/access/... ./service/netquery/...
```

### go vet (statische Analyse)

```bash
go vet ./...
```

---

## 5 · Manuelle Telemetrie-Prüfung

Sicherstellen dass keine bekannten Safing-Telemetrie-URLs mehr im Code sind:

```bash
# Sollte keine Ausgabe liefern:
grep -r 'account.safing.io\|updates.safing.io\|telemetry.safing.io\|api.safing.io' \
  ./service/sync/ ./spn/access/

echo "Exit $?: 0 = sauber, alles andere = Fund"
```

Sicherstellen dass `service/netquery` keine SPN-Pakete importiert:

```bash
# Sollte keine Ausgabe liefern:
grep -r 'safing/portmaster/spn' ./service/netquery/

echo "Exit $?: 0 = sauber"
```

---

## 6 · Desktop-Frontend bauen (Angular 16, Node 20)

Das produktive UI ist die Angular-App unter `desktop/angular`. Die gesamte
Account-/SPN-Logik ist zentral in `projects/safing/portmaster-api/src/lib/spn.service.ts`
neutralisiert (statisches lokales Profil, SPN dauerhaft `disabled`, keine HTTP-
Aufrufe an Account-Endpunkte). Details: `desktop/CHANGES.md`.

```bash
cd desktop/angular

# Abhängigkeiten exakt nach package-lock.json installieren
npm ci --no-audit --no-fund

# Bibliotheken (@safing/ui, @safing/portmaster-api) + App als Production-Build
npm run build
```

Erwartung: Exit-Code 0. Upstream-Warnungen zu CommonJS-Abhängigkeiten
(`js-yaml-loader`, `data-urls`) und zum überschrittenen Bundle-Budget sind
bekannt und unkritisch.

---

## 7 · Frontend-Guards

```bash
cd desktop/angular

# API-Bibliothek darf keine Account-Endpunkte mehr ansprechen (keine Ausgabe erwartet):
grep -rnE 'v1/spn/account|v1/account/features|account\.safing\.io|core:spn/account' \
  projects/safing/portmaster-api/src

# Production-Bundle darf keine Account-API-Aufrufe enthalten (keine Ausgabe erwartet):
grep -lE 'v1/spn/account|v1/account/features' dist/*.js
```

Verbleibende `account.safing.io`-Strings im Bundle stammen aus nicht mehr
gerenderten Komponenten (`spn-login`, `spn-account-details`); sie lösen keine
Requests aus.

---

## 7b · Windows-Installer unter Linux bauen (Cross-Build)

Die Tauri-CLI verweigert `--bundles nsis` auf Nicht-Windows-Hosts, der Bundler-Code
unterstützt es aber. `packaging/windows/render_nsis_linux.py` rendert das originale
Tauri-NSIS-Template (inkl. `templates/nsis/install_hooks.nsh` für Dienst, Treiber und
Intel-Daten) und ruft `makensis` auf.

```bash
# Toolchain
sudo apt install nsis mingw-w64 lld llvm
curl https://sh.rustup.rs -sSf | sh -s -- -y -t x86_64-pc-windows-gnu
# tauri-cli 2.2.7 (prebuilt): https://github.com/tauri-apps/tauri/releases/tag/tauri-cli-v2.2.7

# 1. Frontend
cd desktop/angular && npm ci && npm run build && NODE_ENV=production npx ng build --configuration production tauri-builtin
(cd dist && zip -r ../../../dist/binary/all/portmaster.zip ./ -x "tauri-builtin/*")
(cd ../../assets/data && zip -r -9 -X ../../dist/binary/all/assets.zip *)

# 2. Core (Upstream-Konvention: CGO_ENABLED=0)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags="-X github.com/safing/portmaster/base/info.version=<ver> ..." \
  -o dist/binary/windows_amd64/portmaster-core.exe ./cmds/portmaster-core

# 3. Signierter Kext + core.dll aus dem Upstream-Stable, Intel-Daten
go build -o /tmp/updatemgr ./cmds/updatemgr
/tmp/updatemgr download https://updates.safing.io/stable.v3.json --platform windows_amd64 dist/downloaded/windows_amd64
/tmp/updatemgr download https://updates.safing.io/intel.v3.json dist/intel

# 4. Tauri-UI (portmaster.exe + WebView2Loader.dll)
cd desktop/tauri/src-tauri && cargo tauri build --ci --target x86_64-pc-windows-gnu --no-bundle

# 5. Dateien bereitstellen und Installer bauen
mkdir -p binary intel
cp ../../../dist/downloaded/windows_amd64/{portmaster-kext.sys,portmaster-core.dll} \
   ../../../dist/binary/windows_amd64/portmaster-core.exe ../../../dist/binary/all/*.zip \
   target/x86_64-pc-windows-gnu/release/WebView2Loader.dll binary/
cp ../../../dist/intel/* intel/
cd ../../.. && python3 packaging/windows/render_nsis_linux.py <ver>
# → dist/windows_amd64/portmasterpro_<ver>_x64-setup.exe
```

Hinweise:
- Der Kernel-Treiber (`portmaster-kext.sys`) ist unverändert Safings signierter Treiber
  (Stable 2.2.3, FileVersion 2.1.1.0). Das Split-Tunnel-Feature aus `development`
  benötigt einen neueren Treiber und bleibt standardmäßig aus (`splittun/enable=false`).
- Installer und Binaries sind **nicht signiert** → SmartScreen-Warnung beim ersten Start.
- Der Installer ersetzt eine bestehende Portmaster-Installation (gleicher Dienstname
  `PortmasterCore`, gleiches Installationsverzeichnis).

---

## 8 · Vollständiger Check-Ablauf vor dem Merge

Diesen Block einmalig von oben nach unten durchlaufen:

```bash
# 1. Sauber starten
git checkout no-spn-no-telemetry
git pull

# 2. Deps aufräumen
go mod tidy

# 3. Kompilieren
go build ./...

# 4. Vet
go vet ./...

# 5. Tests (CI-Konvention) + Race-Detector auf den privacy-kritischen Paketen
go test -short -count=1 ./...
go test -race -short -count=1 ./service/network/... ./service/netquery/... ./service/sync/... ./spn/access/...

# 6. Telemetrie-URLs prüfen
grep -r 'account.safing.io\|updates.safing.io\|api.safing.io' \
  ./service/sync/ ./spn/access/ && echo "FUND!" || echo "Sauber."

# 7. SPN-/Account-Imports prüfen
grep -r 'safing/portmaster/spn' ./service/netquery/ && echo "FUND!" || echo "Sauber."
grep -rn --include='*.go' '"github.com/safing/portmaster/spn/access' ./service/ && echo "FUND!" || echo "Sauber."
grep -rn --include='*.go' 'RequiresFeatureIDAnnotation:' ./service/ && echo "FUND!" || echo "Sauber."

# 8. Frontend (Angular, siehe Hinweis in Abschnitt 6)
cd desktop/angular && npm ci && npm run build
```

Wenn alle Schritte ohne Fehler und ohne "FUND!" durchlaufen → Branch ist merge-ready.
Für ein Release zusätzlich die Checkliste in `RELEASE.md` abarbeiten; ein Tag `vX.Y.Z[-rcN]`
erzeugt das GitHub-Release mit Installer und Checksummen automatisch (`release-windows-installer.yml`).

---

## Troubleshooting

### `undefined: mgr.Manager` beim Kompilieren
Die Stub-Dateien importieren `service/mgr`. Stelle sicher dass du auf dem
richtigen Branch bist und `go mod tidy` ausgeführt hast.

```bash
git branch          # muss 'no-spn-no-telemetry' zeigen
go mod tidy
go build ./...
```

### `cgo: C compiler not found`

```bash
# Ubuntu/Debian
sudo apt install build-essential

# Fedora/Rocky
sudo dnf groupinstall 'Development Tools'
```

### `pnpm: command not found`

```bash
npm install -g pnpm
```

### Test schlägt fehl mit `only one instance allowed`
Das `shimLoaded`-Flag in `netquery` ist prozessweit. Tests einzeln ausführen:

```bash
go test -v -count=1 -run TestNetqueryEndpointPathsPresent ./service/netquery/
```
