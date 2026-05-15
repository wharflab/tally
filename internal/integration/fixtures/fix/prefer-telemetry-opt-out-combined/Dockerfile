FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS base
	RUN echo base

FROM node:22 AS web

	WORKDIR /app

	COPY package.json ./package.json

	RUN npm run build

FROM node:22 AS tools

	# bootstrap tooling
	RUN bun install && curl -fsSL https://example.com/install.sh | bash

# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS win

	RUN powershell -Command Write-Host hi
	RUN powershell -Command Write-Host bye
	RUN bootstrap-vcpkg.bat
