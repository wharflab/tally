FROM alpine:3.20 AS base

	RUN echo base

FROM node:22 AS web

# [tally] settings to opt out from telemetry
ENV NEXT_TELEMETRY_DISABLED=1

	WORKDIR /app

	COPY package.json ./package.json

	RUN npm run build

FROM node:22 AS tools

# [tally] settings to opt out from telemetry
ENV DO_NOT_TRACK=1

	# bootstrap tooling
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl

COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

	RUN --mount=type=cache,target=/root/.bun/install/cache,id=bun bun install && curl -fsSL https://example.com/install.sh | bash

# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS win

# [tally] settings to opt out from telemetry
ENV POWERSHELL_TELEMETRY_OPTOUT=1 VCPKG_DISABLE_METRICS=1

	SHELL ["powershell", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true; $ProgressPreference = 'SilentlyContinue';"]

	RUN <<-EOF
		$ErrorActionPreference = 'Stop'
		$PSNativeCommandUseErrorActionPreference = $true
		Write-Host hi
		Write-Host bye
		bootstrap-vcpkg.bat
		EOF
