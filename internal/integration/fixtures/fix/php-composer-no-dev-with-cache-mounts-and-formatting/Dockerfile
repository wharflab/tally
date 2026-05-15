FROM alpine@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS base
RUN echo base

FROM php:8.4-cli@sha256:026ca9f219631274dc8ad045935485c9b9f3d4f44b2ec96649b64a07dba3bf1c AS app
RUN composer  install && echo done
