FROM alpine AS base
	RUN echo base

FROM php:8.4-cli AS app
	RUN --mount=type=cache,target=/root/.cache/composer,id=composer composer install --no-dev \
	&& echo done
