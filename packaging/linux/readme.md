# Installation scripts order

Execution order of installation scripts (`preInstallScript, preRemoveScript, postInstallScript, postRemoveScript`) is different for DEB and RPM packages.
**NOTE!** 'remove' scripts is using from old version!

## DEB scripts order

Useful link: https://wiki.debian.org/MaintainerScripts
```
DEB (apt) Install v2.2.2:
   [*] Before install (2.2.2 : deb : install)
   [*] After  install (2.2.2 : deb : configure)

DEB (apt) Upgrade v1.1.1 -> v2.2.2:
   [*] Before remove  (1.1.1 : deb : upgrade)
   [*] Before install (2.2.2 : deb : upgrade)
   [*] After  remove  (1.1.1 : deb : upgrade)
   [*] After  install (2.2.2 : deb : configure)

 DEB (apt) Remove:
   [*] Before remove  (1.1.1 : deb : remove)
   [*] After  remove  (1.1.1 : deb : remove)
```

## RPM scripts order

Useful link: https://docs.fedoraproject.org/en-US/packaging-guidelines/Scriptlets/

When scriptlets are called, they will be supplied with an argument.
This argument, accessed via $1 (for shell scripts) is the number of packages of this name
which will be left on the system when the action completes.

```
 RPM (dnf) install:
   [*] Before install (2.2.2 : rpm : 1)
   [*] After  install (2.2.2 : rpm : 1)

 RPM (dnf) upgrade:
   [*] Before install (2.2.2 : rpm : 2)
   [*] After  install (2.2.2 : rpm : 2)
   [*] Before remove  (1.1.1 : rpm : 1)
   [*] After  remove  (1.1.1 : rpm : 1)

 RPM (dnf) remove:
   [*] Before remove  (2.2.2 : rpm : 0)
   [*] After  remove  (2.2.2 : rpm : 0)
```