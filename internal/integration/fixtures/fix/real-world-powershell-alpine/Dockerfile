FROM mcr.microsoft.com/powershell:6.2.4-alpine-3.8@sha256:1b6d8a5320abc052b439d083723cf125336f37b0b75b4441b41c3ad6ea006990
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]

WORKDIR /app

RUN apk add --update nodejs nodejs-npm
RUN Install-Module -Name Az -AllowClobber -Force
RUN Set-PSRepository -Name PSGallery -InstallationPolicy Trusted; \
    Install-Module Configuration -RequiredVersion 1.3.1 -Repository PSGallery -Scope AllUsers -Verbose; \
    Install-Module PSSlack -RequiredVersion 1.0.2 -Repository PSGallery -Scope AllUsers -Verbose;

SHELL ["/bin/ash", "-eo", "pipefail", "-c"]

RUN apk add --no-cache bind-tools gnupg git tini

RUN (curl -Ls https://cli.doppler.com/install.sh || wget -qO- https://cli.doppler.com/install.sh) | sh
RUN npm clean-install --only=production --silent --no-audit && mv node_modules ../
