# Release-Checkliste (portmasterpro)

Ziel: reproduzierbares, geprüftes Release ohne Überraschungen. Alle Punkte abhaken.

## 1 · Code einfrieren
- [ ] Upstream `development` gemergt (`git fetch upstream && git merge upstream/development`), Konflikte gelöst, keine SPN/Account-Rückstände:
      `grep -RIn 'account.safing.io\|/v1/spn/account\|/v1/account/features' service spn desktop/angular/projects` → leer
- [ ] `go build ./... && go vet ./... && go test -short -count=1 ./...` lokal grün (bekannte Umgebungsfehler laut BUILDING.md §5 ausgenommen)
- [ ] `desktop/angular`: `npm ci && npm run build` grün, Bundle-Guard leer (BUILDING.md §7)
- [ ] `CHANGELOG.md`: Abschnitt `## [X.Y.Z] - YYYY-MM-DD` aus `[Unreleased]` erzeugt
- [ ] Version gesetzt: `desktop/tauri/src-tauri/Cargo.toml` (`version = "X.Y.Z"`) und `Cargo.lock` (Paket `portmaster`)
- [ ] `release-candidate/X.Y.Z` Branch vom Stand erstellt, PR gegen `development` offen, **alle** Workflows grün
      (No-Telemetry CI, Go, Angular, Windows Kernel Extension, Tauri)

## 2 · Release Candidate
- [ ] Tag `vX.Y.Z-rcN` auf den eingefrorenen Commit pushen → Workflow erstellt Pre-Release + Assets automatisch
- [ ] Installer auf einer **echten Windows-Maschine** testen:
  - [ ] Installation über bestehende Portmaster-/portmasterpro-Installation (Einstellungen bleiben)
  - [ ] Dienst `PortmasterCore` startet, Treiber lädt (Log: `windowskext2`), UI verbindet
  - [ ] Network History bleibt über Neustart erhalten; Bandbreiten-Charts füllen sich
  - [ ] Keine Verbindung zu `account.safing.io`; nur `updates.safing.io` für Intel-Daten
  - [ ] Intel-Update läuft durch (`filterlists: ... update complete`)
  - [ ] Deinstallation entfernt Dienst, Treiber und Autostart-Eintrag
- [ ] Linux-Core-Smoke (`--disable-interception`, unprivilegiert): alle Module starten/stoppen sauber
- [ ] Mindestens 7 Tage Betrieb ohne Blocker-Report

## 3 · Full Release
- [ ] Nur Fixes seit RC, kein neues Feature; letzte Fixes als `-rcN+1` erneut durchlaufen
- [ ] Tag `vX.Y.Z` (ohne Suffix) pushen → Workflow erstellt das finale Release
- [ ] Release-Seite prüfen: 5 Assets, `SHA256SUMS.txt` stimmt mit lokal geprüftem Installer überein
- [ ] PR nach `development` mergen (oder Default-Branch auf die Fork-Mainline setzen, siehe unten)
- [ ] Projekt-Wiki/Memory aktualisieren

## Hinweise
- GitHub startet `workflow_dispatch`- und `schedule`-Workflows nur vom **Default-Branch**. Solange `development`
  der Upstream-Spiegel ist, müssen Workflow-Änderungen dorthin gespiegelt werden. Empfehlung: Default-Branch
  auf die Fork-Mainline umstellen (z. B. `main` = heutiger `no-spn-no-telemetry`).
- Upstream-Treiber: Split Tunnel benötigt einen neueren signierten Kext als Stable 2.2.3. Erst aktivieren,
  wenn Upstream einen passenden signierten Treiber veröffentlicht hat.
- Signatur: ohne Code-Signing-Zertifikat warnt SmartScreen. Option: eigenes Zertifikat
  (`packaging/windows/sign_binaries_in_dist.ps1`) — der Kext muss weiterhin der signierte Upstream-Treiber bleiben.
