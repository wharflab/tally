FROM alpine:3.20
CMD ["serve"]
RUN echo hello
HEALTHCHECK CMD curl -f http://localhost/
ENTRYPOINT ["/app"]
