!macro NSIS_HOOK_PREINSTALL
  ; Current working directory is <project-dir>\desktop\tauri\src-tauri\target\release\nsis\x64

  SetOutPath "$INSTDIR"

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

!macro NSIS_HOOK_POSTINSTALL
  ExecWait 'sc.exe create PortmasterCore binPath= "$INSTDIR\portmaster-core.exe"'
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  ExecWait 'sc.exe stop PortmasterCore'
  ExecWait 'sc.exe delete PortmasterCore'
!macroend

