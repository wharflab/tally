FROM ubuntu:22.04
RUN pwsh -Command "$ProgressPreference = 'SilentlyContinue'; Invoke-WebRequest https://example.com/c.zip -OutFile /tmp/c.zip"
