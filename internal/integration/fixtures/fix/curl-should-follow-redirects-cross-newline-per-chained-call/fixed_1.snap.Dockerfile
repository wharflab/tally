FROM ubuntu:22.04
RUN curl --location -fsSo /tmp/file https://example.com/file \
	&& chmod +x /tmp/file
