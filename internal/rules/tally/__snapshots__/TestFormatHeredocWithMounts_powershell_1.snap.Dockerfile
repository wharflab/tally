RUN <<EOF
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Invoke-WebRequest https://example.com/app.zip -OutFile C:\temp\app.zip
Expand-Archive C:\temp\app.zip -DestinationPath C:\tools
Remove-Item C:\temp\app.zip -Force
EOF