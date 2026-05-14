# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command `
    $ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true; Invoke-WebRequest -Uri https://example.com/setup.exe -OutFile C:\setup.exe; `
    Start-Process C:\setup.exe -Wait; `
    Remove-Item C:\setup.exe -Force
