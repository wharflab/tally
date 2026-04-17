# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command `
    $ProgressPreference = 'SilentlyContinue'; Invoke-WebRequest -Uri https://example.com/setup.exe -OutFile C:\setup.exe
