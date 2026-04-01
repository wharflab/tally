RUN <<EOF
$ErrorActionPreference = 'Stop'
Invoke-WebRequest https://example.com/app.zip -OutFile C:\temp\app.zip
if (-not $?) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; exit 1 }
Expand-Archive C:\temp\app.zip -DestinationPath C:\tools
if (-not $?) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; exit 1 }
Remove-Item C:\temp\app.zip -Force
EOF