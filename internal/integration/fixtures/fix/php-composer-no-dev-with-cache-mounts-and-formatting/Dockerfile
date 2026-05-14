FROM alpine AS base
RUN echo base

FROM php:8.4-cli AS app
RUN composer  install && echo done
