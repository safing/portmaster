# Install service
Execute this step after all files are copied:
```
  ExecWait 'sc.exe create PortmasterCore type= own binPath= "$INSTDIR\portmaster-core.exe --data=C:\Dev\test_data --devmode -log debug --service" DisplayName= "Portmaster Core"'
```
# Stop and uninstall service 
execute this step at the beginning of the uninstall process
```
  ExecWait 'sc.exe stop PortmasterCore'
  ExecWait 'sc.exe delete PortmasterCore'
```