#escape=`
FROM teeks99/msvc-win:14.0

  # [tally] settings to opt out from telemetry
  ENV POWERSHELL_TELEMETRY_OPTOUT=1

SHELL ["powershell", "-command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;", "$ProgressPreference = 'SilentlyContinue';"]

# Install Chocolatey
RUN <<EOF
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
iex ((New-Object net.webclient).DownloadString('https://chocolatey.org/install.ps1'))
choco feature disable --name showDownloadProgress
choco feature enable --name allowGlobalConfirmation
EOF

WORKDIR /app

COPY Packages.config Packages.config

WORKDIR /app

COPY user-config.jam user-config.jam

RUN <<EOF
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
choco install Packages.config
Invoke-WebRequest https://www.python.org/ftp/python/3.9.0/python-3.9.0.exe -OutFile python-3.9.0.exe
Start-Process -FilePath 'python-3.9.0.exe' -ArgumentList '/quiet', 'TargetDir=C:\Python39-32\', 'CompileAll=1', 'PrependPath=0' -PassThru -Wait
Remove-Item -Path python-3.9.0.exe -Force
setx /M PYTHONIOENCODING UTF-8
del .\Packages.config
move .\user-config.jam $env:USERPROFILE
cd $env:USERPROFILE
Invoke-WebRequest https://curl.haxx.se/ca/cacert.pem -OutFile $env:USERPROFILE\cacert.pem
$plaintext_pw = 'PASSWORD'
$secure_pw = ConvertTo-SecureString $plaintext_pw -AsPlainText -Force
$openssl_pw = '-passout pass:' + $plaintext_pw
Start-Process -FilePath 'C:\Program Files\OpenSSL-Win64\bin\openssl.exe' -ArgumentList 'pkcs12', '-export', '-nokeys', '-out certs.pfx', '-in cacert.pem', $openssl_pw -PassThru -Wait
Import-PfxCertificate -Password $secure_pw -CertStoreLocation Cert:\LocalMachine\Root -FilePath certs.pfx
cmd /c 'echo ca_certificate = %USERPROFILE%\cacert.pem > %USERPROFILE%\.wgetrc'
setx /M HOME $env:USERPROFILE
EOF

# Define the entry point for the docker container.
# This entry point adds the developer environemnt and starts the command shell
ENTRYPOINT ["C:\\Program Files (x86)\\Microsoft Visual Studio 14.0\\Common7\\Tools\\VsDevCmd.bat", `
    "&&", "cmd.exe"]
