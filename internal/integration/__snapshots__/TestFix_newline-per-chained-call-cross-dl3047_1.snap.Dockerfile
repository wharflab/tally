FROM ubuntu:24.04
RUN wget --progress=dot:giga https://example.com/file.tar.gz \
	&& tar -xzf file.tar.gz
