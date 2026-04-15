# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Invoke-WebRequest -Uri https://example.com/setup.exe `
      -OutFile C:\setup.exe; `
    Start-Process C:\setup.exe -Wait; `
    Remove-Item C:\setup.exe -Force
