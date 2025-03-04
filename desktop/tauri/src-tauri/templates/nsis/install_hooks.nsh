!include LogicLib.nsh

!addplugindir "..\..\..\..\templates\NSIS_Simple_Service_Plugin_Unicode_1.30"

var oldInstallationDir
var dataDir

!macro NSIS_HOOK_PREINSTALL
  ; Abort if old service is running
  SimpleSC::ServiceIsStopped "PortmasterCore"
  Pop $0
  Pop $1
  ${If} $0 == 0
    ${If} $1 == 0
      MessageBox MB_OK "Portmaster service is running. Stop it and run the installer again."
      Abort
    ${EndIf}
  ${EndIf}

  File "..\..\..\..\binary\index.json"
  File "..\..\..\..\binary\portmaster-core.exe"
  File "..\..\..\..\binary\portmaster-kext.sys"
  File "..\..\..\..\binary\portmaster-core.dll"
  File "..\..\..\..\binary\WebView2Loader.dll"
  File "..\..\..\..\binary\portmaster.zip"
  File "..\..\..\..\binary\assets.zip"

  SetOutPath "$COMMONPROGRAMDATA\Portmaster\intel"

  File "..\..\..\..\intel\index.json"
  File "..\..\..\..\intel\base.dsdl"
  File "..\..\..\..\intel\geoipv4.mmdb"
  File "..\..\..\..\intel\geoipv6.mmdb"
  File "..\..\..\..\intel\index.dsd"
  File "..\..\..\..\intel\intermediate.dsdl"
  File "..\..\..\..\intel\urgent.dsdl"

  ; restire previous state
  SetOutPath "$INSTDIR"

!macroend

;--------------------------------------------------
; Post-install hook:
; - Remove old service
; - Installs the service
!macro NSIS_HOOK_POSTINSTALL
  DetailPrint "Installing service"
  ; Remove old service
  SimpleSC::RemoveService "PortmasterCore"
  Pop $0
  ${If} $0 != 0
    DetailPrint "Failed to remove PortmasterCore service. Error: $0"
  ${EndIf}

  ; Install the service:
  ; Parameters:
  ;   1. Service Name: "PortmasterCore"
  ;   2. Display Name: "Portmaster Core"
  ;   3. Service Type: "16" for SERVICE_WIN32_OWN_PROCESS
  ;   4. Start Type: "2" for SERVICE_AUTO_START
  ;   5. Binary Path: Executable with arguments.
  ;   6 & 7. Dependencies and account info (empty uses defaults).
  SimpleSC::InstallService "PortmasterCore" "Portmaster Core" "16" "2" "$INSTDIR\portmaster-core.exe --log-dir=%PROGRAMDATA%\Portmaster\logs" "" "" ""
  Pop $0  ; returns error code (0 on success)
  ${If} $0 != 0
    SimpleSC::GetErrorMessage $0
    Pop $0
    MessageBox MB_OK "Service creation failed. Error: $0"
    Abort
  ${EndIf}

  SimpleSC::SetServiceDescription "PortmasterCore" "Portmaster Application Firewall - Core Service"

  StrCpy $oldInstallationDir "$COMMONPROGRAMDATA\Safing\Portmaster"
  StrCpy $dataDir "$COMMONPROGRAMDATA\Portmaster"

  ; Check if the folder exists
  IfFileExists "$oldInstallationDir\*.*" 0 Finish

  ; Stop if the migration flag(file) already exists.
  IfFileExists "$oldInstallationDir\migrated.txt" Finish 0

  ; Copy files
  DetailPrint "Migrating config from old installation: $oldInstallationDir"

  CreateDirectory "$dataDir"
  CreateDirectory "$dataDir\databases"
  CopyFiles "$oldInstallationDir\config.json" "$dataDir"
  CopyFiles "$oldInstallationDir\databases\*.*" "$dataDir\databases"

  ; Create empty file to indicate that the data has already been migrated.
  FileOpen $0 "$oldInstallationDir\migrated.txt" w
  FileClose $0

  ; Delete v1 shortcuts
  RMDir /r "$SMPROGRAMS\Portmaster"
  Delete "$SMSTARTUP\Portmaster Notifier.lnk"

  ; Delete v1 uninstaller
  Delete "$oldInstallationDir\portmaster-uninstaller.exe"

  ; Delete v1 user shortuct if there.
  SetShellVarContext current
  Delete "$AppData\Microsoft\Windows\Start Menu\Programs\Portmaster.lnk"
  SetShellVarContext all

  Finish:

!macroend

;--------------------------------------------------
; Pre-uninstall hook:
; - Stops and removes the service.
!macro NSIS_HOOK_PREUNINSTALL

  DetailPrint "Stopping service"
  SimpleSC::StopService "PortmasterCore" "1" "30"
  Pop $0
  ${If} $0 != 0
    DetailPrint "Failed to stop PortmasterCore service. Error: $0"
  ${Else}
    DetailPrint "Service PortmasterCore stopped successfully."
  ${EndIf}

  DetailPrint "Removing service"
  SimpleSC::RemoveService "PortmasterCore"
  Pop $0
  ${If} $0 != 0
    DetailPrint "Failed to remove PortmasterCore service. Error: $0"
  ${Else}
    DetailPrint "Service PortmasterCore removed successfully."
  ${EndIf}
!macroend

