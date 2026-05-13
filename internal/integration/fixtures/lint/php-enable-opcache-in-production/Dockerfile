FROM composer:2 AS deps
WORKDIR /app
COPY composer.json composer.lock ./
RUN composer install --no-dev --no-interaction --optimize-autoloader

FROM php:8.4-fpm AS app
WORKDIR /app
COPY --from=deps /app/vendor /app/vendor
COPY . .
