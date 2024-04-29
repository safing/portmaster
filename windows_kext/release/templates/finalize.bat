@echo off
set DISTDIR=dist\windows_amd64\kext
set SIGNEDDIR=Signed\drivers\PortmasterKext

echo.
echo =====
echo copying files ...
mkdir %DISTDIR%
echo copy %SIGNEDDIR%\PortmasterKext64.sys %DISTDIR%\portmaster-kext_vX-X-X.sys
copy %SIGNEDDIR%\PortmasterKext64.sys %DISTDIR%\portmaster-kext_vX-X-X.sys

echo.
echo =====
echo OPTIONAL:
echo YOUR TURN: sign .sys (add your sig for additional transparency)
echo use something along the lines of:
echo.
echo signtool sign /sha1 C2CBB3A0256A157FEB08B661D72BF490B68724C4 /tr http://timestamp.digicert.com /td sha256 /fd sha256 /a /as %DISTDIR%\portmaster-kext_vX-X-X.sys
echo.

echo.
echo =====
echo YOUR TURN: rename %DISTDIR%\portmaster-kext-vX-X-X.sys to correct versions!
echo DONE!
echo.
