FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS base

	RUN echo base

FROM node:22@sha256:e3ca095133ba41a0a73b009f19e4253f1a878e70bb9499f6a9d21b19d082bd91 AS web

# [tally] settings to opt out from telemetry
ENV NEXT_TELEMETRY_DISABLED=1

	WORKDIR /app

	COPY package.json ./package.json

	RUN npm run build

FROM node:22@sha256:e3ca095133ba41a0a73b009f19e4253f1a878e70bb9499f6a9d21b19d082bd91 AS tools

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
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS win

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
