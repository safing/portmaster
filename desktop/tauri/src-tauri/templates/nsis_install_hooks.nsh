!define NSIS_HOOK_POSTINSTALL "NSIS_HOOK_POSTINSTALL_"

!macro NSIS_HOOK_POSTINSTALL_
  ExecWait '"$INSTDIR\portmaster-start.exe" install core-service --data="$INSTDIR\data"'
!macroend


!define NSIS_HOOK_PREUNINSTALL "NSIS_HOOK_PREUNINSTALL_"

!macro NSIS_HOOK_PREUNINSTALL_
  ExecWait 'sc.exe stop PortmasterCore'
  ExecWait 'sc.exe delete PortmasterCore'
!macroend

