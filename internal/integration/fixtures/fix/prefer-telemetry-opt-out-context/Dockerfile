FROM node:22@sha256:e3ca095133ba41a0a73b009f19e4253f1a878e70bb9499f6a9d21b19d082bd91 AS web
WORKDIR /app
COPY package.json ./package.json
RUN npm run build

FROM python:3.12@sha256:ed942629d18ad03521f9835ff95f3edbfbe99ccd38be6ba64a509ce3c1b149a8 AS ml
WORKDIR /srv
COPY requirements.txt ./requirements.txt
RUN pip install -r requirements.txt

FROM node:22@sha256:e3ca095133ba41a0a73b009f19e4253f1a878e70bb9499f6a9d21b19d082bd91 AS classic
RUN yarn install

# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS win
RUN powershell -Command Write-Host hi
RUN bootstrap-vcpkg.bat
