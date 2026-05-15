# Linux: useradd -G establishes supplementary groups; USER app:app drops them.
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS linux-fires
RUN groupadd -r docker && \
    useradd -r -g app -G docker,wheel app
USER app:app

# Linux no-fire: bare USER app preserves supplementary groups.
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS linux-clean
RUN useradd -r -g app -G docker app
USER app

# Linux FROM-ancestry: supplementary groups established in parent stage,
# explicit group specified in child stage.
FROM linux-fires AS linux-ancestry
USER app:app
CMD ["app"]

# Windows cmd: net localgroup /add establishes membership; USER drops it.
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS windows-cmd-fires
RUN net user app password /add && net localgroup docker app /add
USER app:docker

# Windows PowerShell: Add-LocalGroupMember establishes membership;
# USER app:docker drops it.
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS windows-ps-fires
SHELL ["pwsh", "-Command"]
RUN New-LocalUser -Name app -NoPassword -AccountNeverExpires ; Add-LocalGroupMember -Group docker -Member app
USER app:docker
