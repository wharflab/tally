# Source: https://github.com/jaraco/jaraco.windows/blob/455a6c3e22082b9b43684bba2f7b808f13db41c3/Dockerfile
# Install Visual Studio Build tools based on
# https://docs.microsoft.com/en-us/visualstudio/install/build-tools-container?view=vs-2019

FROM mcr.microsoft.com/dotnet/framework/sdk:4.8-windowsservercore-ltsc2019

# [tally] settings to opt out from telemetry
ENV POWERSHELL_TELEMETRY_OPTOUT=1

# Download the Build Tools bootstrapper.
ADD https://aka.ms/vs/16/release/vs_buildtools.exe vs_buildtools.exe

SHELL ["powershell","-Command","$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true; $ProgressPreference = 'SilentlyContinue';"]

# Install chocolatey
RUN <<EOF
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-ExecutionPolicy Bypass -Scope Process -Force
[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
iwr https://chocolatey.org/install.ps1 -UseBasicParsing | iex
choco feature enable -n allowGlobalConfirmation
choco install git pypy3 python
python -m pip install -U pip pipx pip-run
setx path "$env:Path;C:\Users\ContainerAdministrator\.local\bin;C:\programdata\chocolatey\lib\pypy3\tools\pypy3.7-v7.3.4-win32;C:\programdata\chocolatey\lib\pypy3\tools\pypy3.7-v7.3.4-win32\Scripts"
pipx install tox
pipx install httpie
pypy3 -m ensurepip
pypy3 -m pip install -U pip
cmd /c 'certutil -generateSSTFromWU roots.sst && certutil -addstore -f root roots.sst && del roots.sst'
setx TOX_WORK_DIR \tox
EOF

# Install Visual Studio
WORKDIR /app

COPY . jaraco.windows

RUN py -m pip-run -q ./jaraco.windows -- -m jaraco.windows.msvc

ENTRYPOINT ["powershell.exe", "-NoLogo", "-ExecutionPolicy", "Bypass"]
