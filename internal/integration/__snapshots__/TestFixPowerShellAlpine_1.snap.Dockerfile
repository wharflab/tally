FROM mcr.microsoft.com/powershell:6.2.1-alpine-3.8

# [tally] settings to opt out from telemetry
ENV POWERSHELL_TELEMETRY_OPTOUT=1

SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]

WORKDIR /app

# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl

COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF

# [tally] wget configuration for improved robustness
ENV WGETRC=/etc/wgetrc

COPY --chmod=0644 <<EOF ${WGETRC}
retry_connrefused = on
timeout = 15
tries = 5
EOF

RUN --mount=type=cache,target=/var/cache/apk,id=apk,sharing=locked apk add --update nodejs nodejs-npm
RUN <<EOF
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Install-Module -Name Az -AllowClobber -Force
Set-PSRepository -Name PSGallery -InstallationPolicy Trusted
Install-Module Configuration -RequiredVersion 1.3.1 -Repository PSGallery -Scope AllUsers -Verbose
Install-Module PSSlack -RequiredVersion 1.0.2 -Repository PSGallery -Scope AllUsers -Verbose
EOF

SHELL ["/bin/ash", "-eo", "pipefail", "-c"]

RUN --mount=type=cache,target=/var/cache/apk,id=apk,sharing=locked apk add bind-tools gnupg git tini
RUN <<EOF
set -e
set -o pipefail
(curl -Ls https://cli.doppler.com/install.sh || wget -qO- https://cli.doppler.com/install.sh) | sh
npm clean-install --only=production --silent --no-audit
mv node_modules ../
EOF
