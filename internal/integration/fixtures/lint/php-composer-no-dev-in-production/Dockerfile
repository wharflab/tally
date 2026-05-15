FROM composer:2@sha256:1364b5b9132ab4c42ea3be53e894572c32fe75a512cb3b1c3903fcc9bce53dcc AS deps
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install

FROM composer:2@sha256:1364b5b9132ab4c42ea3be53e894572c32fe75a512cb3b1c3903fcc9bce53dcc AS dev
WORKDIR /app
RUN composer install

FROM php:8.4-cli@sha256:026ca9f219631274dc8ad045935485c9b9f3d4f44b2ec96649b64a07dba3bf1c AS runtime
ENV COMPOSER_NO_DEV=1
RUN composer install

FROM php:8.4-cli@sha256:026ca9f219631274dc8ad045935485c9b9f3d4f44b2ec96649b64a07dba3bf1c AS final
WORKDIR /app
RUN composer install --no-interaction
