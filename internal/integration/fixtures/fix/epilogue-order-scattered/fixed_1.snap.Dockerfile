FROM alpine:3.20
RUN echo hello

HEALTHCHECK CMD curl -f http://localhost/
ENTRYPOINT ["/app"]
CMD ["serve"]
