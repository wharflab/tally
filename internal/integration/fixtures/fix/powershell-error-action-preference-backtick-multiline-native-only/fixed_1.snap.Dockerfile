# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185
SHELL ["powershell", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Invoke-WebRequest -Uri https://example.com/setup.exe `
      -OutFile C:\setup.exe; `
    Start-Process C:\setup.exe -Wait; `
    Remove-Item C:\setup.exe -Force
