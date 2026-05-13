FROM mcr.microsoft.com/powershell:6.2.1-alpine-3.8
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
