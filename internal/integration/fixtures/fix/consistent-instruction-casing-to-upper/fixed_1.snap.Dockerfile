FROM alpine:3.18
RUN echo hello
COPY . /app
WORKDIR /app
