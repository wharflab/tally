# Creates user but never switches to it — violation expected
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
RUN groupadd -r appgroup && useradd -r -g appgroup appuser
CMD ["app"]
