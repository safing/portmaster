!include LogicLib.nsh

!addplugindir "..\..\..\..\templates\NSIS_Simple_Service_Plugin_Unicode_1.30"

var oldInstallationDir
var dataDir

!macro NSIS_HOOK_PREINSTALL
  ; Try to stop the service if it's running
  SimpleSC::ServiceIsStopped "PortmasterCore"
  Pop $0
  Pop $1
  ${If} $0 == 0
    ${If} $1 == 0

      DetailPrint "PortmasterCore service is running. Stopping service ..."
      SimpleSC::StopService "PortmasterCore" 1 60
      Pop $0
      ${If} $0 != 0
        DetailPrint "Failed to stop PortmasterCore service. Error: $0"
        MessageBox MB_OK "PortmasterCore service is running. Stop it and run the installer again."
        Abort
      ${EndIf}
      
      ; wait a little (give change for service to fully stop)
      Sleep 2000

    ${EndIf}
  ${EndIf}

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
  File "..\..\..\..\intel\main-intel.yaml"
  File "..\..\..\..\intel\notifications.yaml"
  File "..\..\..\..\intel\news.yaml"

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

  ; Install the service:
  ; Parameters:
  ;   1. Service Name: "PortmasterCore"
  ;   2. Display Name: "Portmaster Core"
  ;   3. Service Type: "16" for SERVICE_WIN32_OWN_PROCESS
  ;   4. Start Type: "2" for SERVICE_AUTO_START
  ;   5. Binary Path: Executable with arguments.
  ;   6 & 7. Dependencies and account info (empty uses defaults).
  SimpleSC::InstallService "PortmasterCore" "Portmaster Core" 16 2 "$INSTDIR\portmaster-core.exe --log-dir=%PROGRAMDATA%\Portmaster\logs" "" "" ""
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

  ; Delete v1 old binaries
  Delete "$oldInstallationDir\portmaster-uninstaller.exe"
  Delete "$oldInstallationDir\portmaster-start.exe"
  Delete "$oldInstallationDir\portmaster.ico"
  RMDir /r "$oldInstallationDir\exec"
  RMDir /r "$oldInstallationDir\updates"
  RMDir /r "$oldInstallationDir\databases\cache"
  RMDir /r "$oldInstallationDir\intel"

  ; Delete the link to the ProgramData folder
  RMDir /r "$PROGRAMFILES64\Safing"

  ; Delete v1 user shortcut if its there.
  SetShellVarContext current
  Delete "$AppData\Microsoft\Windows\Start Menu\Programs\Portmaster.lnk"
  SetShellVarContext all

  ; Delete v1 registry values
  DeleteRegKey HKLM "SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\Portmaster"

  Finish:

!macroend

;--------------------------------------------------
; Pre-uninstall hook:
; - Stops and removes the service.
!macro NSIS_HOOK_PREUNINSTALL
  DetailPrint "Stopping service"
  ; Trigger service stop. In the worst case the service should stop in ~60 seconds.
  SimpleSC::StopService "PortmasterCore" 1 60
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

;--------------------------------------------------
; Post-uninstall hook:
; - Delete files
!macro NSIS_HOOK_POSTUNINSTALL
  ; Delete binaries
  Delete /REBOOTOK "$INSTDIR\index.json"
  Delete /REBOOTOK "$INSTDIR\portmaster-core.exe"
  Delete /REBOOTOK "$INSTDIR\portmaster-kext.sys"
  Delete /REBOOTOK "$INSTDIR\portmaster-core.dll"
  Delete /REBOOTOK "$INSTDIR\WebView2Loader.dll"
  Delete /REBOOTOK "$INSTDIR\portmaster.zip"
  Delete /REBOOTOK "$INSTDIR\assets.zip"
  RMDir /r /REBOOTOK "$INSTDIR"

  ; delete data files
  Delete  /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\databases\history.db"
  RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\databases\cache"
  RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\databases\icons"
  RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\intel"
  RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\download_intel"
  RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\download_binaries"
  RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\exec"
  RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster\logs"

  ${If} $DeleteAppDataCheckboxState = 1
    DetailPrint "Deleting the application data..."
    RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Portmaster"
    RMDir /r /REBOOTOK "$COMMONPROGRAMDATA\Safing"
  ${Else}
    DetailPrint "Application data kept as requested by the user."
  ${EndIf}

!macroend