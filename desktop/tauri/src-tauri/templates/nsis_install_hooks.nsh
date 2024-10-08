!macro NSIS_HOOK_PREINSTALL
  ; Current working directory is <project-dir>\target\release\nsis\x64

  SetOutPath "$INSTDIR"

  File "..\..\..\..\binaries\bin-index.json"
  File "..\..\..\..\binaries\portmaster-core.exe"
  File "..\..\..\..\binaries\portmaster-kext.sys"
  File "..\..\..\..\binaries\portmaster.zip"
  File "..\..\..\..\binaries\assets.zip"

  SetOutPath "$COMMONPROGRAMDATA\Portmaster\intel"

  File "..\..\..\..\binaries\intel-index.json"
  File "..\..\..\..\binaries\base.dsdl"
  File "..\..\..\..\binaries\geoipv4.mmdb"
  File "..\..\..\..\binaries\geoipv6.mmdb"
  File "..\..\..\..\binaries\index.dsd"
  File "..\..\..\..\binaries\intermediate.dsdl"
  File "..\..\..\..\binaries\urgent.dsdl"

  ; restire previous state
  SetOutPath "$INSTDIR"

!macroend

!macro NSIS_HOOK_POSTINSTALL
  ExecWait 'sc.exe create PortmasterCore binPath= "$INSTDIR\portmaster-core.exe" --data="$COMMONPROGRAMDATA\Portmaster\data"' $0
  IntCmp $0 0 +2
    MessageBox MB_OK "Failed to create PortmasterCore service."
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  ExecWait 'sc.exe stop PortmasterCore' $0
  ; Ignore errors if the service is not running
  ExecWait 'sc.exe delete PortmasterCore' $0
  ; Ignore errors if the service does not exist
!macroend

