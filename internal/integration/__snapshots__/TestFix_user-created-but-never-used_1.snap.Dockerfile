FROM ubuntu:22.04
RUN useradd -r appuser
USER appuser
CMD ["app"]
