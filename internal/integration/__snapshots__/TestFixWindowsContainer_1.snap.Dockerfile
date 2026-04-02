FROM mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019

# [tally] settings to opt out from telemetry
ENV POWERSHELL_TELEMETRY_OPTOUT=1

# Install Chocolatey
RUN @powershell -NoProfile -ExecutionPolicy Bypass -Command "$env:ChocolateyUseWindowsCompression='false'; iex ((New-Object System.Net.WebClient).DownloadString('https://chocolatey.org/install.ps1'))" && SET "PATH=%PATH%;%ALLUSERSPROFILE%\chocolatey\bin"

SHELL ["powershell","-Command","$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true; $ProgressPreference = 'SilentlyContinue';"]

# Install build tools
RUN <<EOF
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
add-windowsfeature web-asp-net45
choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10
choco install dotnet4.6-targetpack --allow-empty-checksums -y
choco install nuget.commandline --allow-empty-checksums -y
nuget install MSBuild.Microsoft.VisualStudio.Web.targets -Version 14.0.0.3
nuget install WebConfigTransformRunner -Version 1.0.0.1
md c:\build
EOF

WORKDIR c:/build

COPY . c:/build

RUN <<EOF
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
remove-item C:\inetpub\wwwroot\iisstart.*
Invoke-WebRequest https://dist.nuget.org/win-x86-commandline/latest/nuget.exe -OutFile c:/build/.nuget/nuget.exe
nuget restore
C:\Windows\Microsoft.NET\Framework64\v4.0.30319\MSBuild.exe /p:Platform="Any CPU" /p:VisualStudioVersion=12.0 /p:VSToolsPath=c:\MSBuild.Microsoft.VisualStudio.Web.targets.14.0.0.3\tools\VSToolsPath TicketDesk2.sln
xcopy c:\build\TicketDesk.Web.Client\* c:\inetpub\wwwroot /s
EOF

# Start application
ENTRYPOINT ["powershell.exe", "./Startup.ps1"]
