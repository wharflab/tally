FROM alpine:3.18 AS Builder
RUN echo hello
FROM alpine:3.18
COPY --from=Builder /app /app
