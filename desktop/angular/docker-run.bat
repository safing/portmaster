@echo off

setlocal

:: Store the current directory
set "current_dir=%cd%"

:: Get base directory for mounting
cd ..\..
set "mnt=%cd%"
:: Return to the original directory
cd /d "%current_dir%"

REM Run Docker container and start dev server
docker run ^
    -ti ^
    --rm ^
    -v %mnt%:/portmaster ^
    -w /portmaster/desktop/angular ^
    -p 8081:8080 ^
    node:latest ^
    npm start -- --host 0.0.0.0 --port 8080

endlocal