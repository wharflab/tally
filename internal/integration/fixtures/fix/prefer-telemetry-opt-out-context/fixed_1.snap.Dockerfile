FROM node:22 AS web
# [tally] settings to opt out from telemetry
ENV NEXT_TELEMETRY_DISABLED=1
WORKDIR /app
COPY package.json ./package.json
RUN npm run build

FROM python:3.12 AS ml
# [tally] settings to opt out from telemetry
ENV HF_HUB_DISABLE_TELEMETRY=1
WORKDIR /srv
COPY requirements.txt ./requirements.txt
RUN pip install -r requirements.txt

FROM node:22 AS classic
RUN yarn install

# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS win
# [tally] settings to opt out from telemetry
ENV POWERSHELL_TELEMETRY_OPTOUT=1 VCPKG_DISABLE_METRICS=1
RUN powershell -Command Write-Host hi
RUN bootstrap-vcpkg.bat
