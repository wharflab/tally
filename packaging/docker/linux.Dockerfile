FROM gcr.io/distroless/base-debian12:nonroot

COPY --chmod=0755 tally /usr/bin/tally

HEALTHCHECK NONE

ENTRYPOINT ["/usr/bin/tally"]
