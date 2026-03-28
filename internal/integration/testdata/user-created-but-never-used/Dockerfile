# Creates user but never switches to it — violation expected
FROM ubuntu:22.04
RUN groupadd -r appgroup && useradd -r -g appgroup appuser
CMD ["app"]
