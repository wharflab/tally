FROM php:8.4-cli@sha256:026ca9f219631274dc8ad045935485c9b9f3d4f44b2ec96649b64a07dba3bf1c AS builder
RUN docker-php-ext-install xdebug

FROM php:8.4-fpm@sha256:121ed120cc0c125e33cd3ad9ccd7cde679e680040b21b58f8b787c28d81d9bce AS app
RUN pecl install xdebug
