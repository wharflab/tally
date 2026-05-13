FROM node:22 AS web
WORKDIR /app
COPY package.json ./package.json
RUN npm run build

FROM python:3.12 AS ml
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
