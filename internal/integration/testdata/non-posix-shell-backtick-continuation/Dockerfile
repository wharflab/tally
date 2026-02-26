# escape=`

FROM mcr.microsoft.com/windows/servercore:ltsc2025

SHELL ["C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe", "-Command"]

RUN Invoke-WebRequest -Uri https://github.com/git-for-windows/git/releases/download/v2.50.0.windows.1/PortableGit-2.50.0-64-bit.7z.exe -OutFile PortableGit.exe; `
    Start-Process -FilePath .\PortableGit.exe -ArgumentList '-o"C:\git"', '-y' -Wait; `
    Remove-Item PortableGit.exe
