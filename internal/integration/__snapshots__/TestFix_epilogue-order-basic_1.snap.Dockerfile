FROM alpine:3.20
RUN echo hello

ENTRYPOINT ["/app"]
CMD ["serve"]
