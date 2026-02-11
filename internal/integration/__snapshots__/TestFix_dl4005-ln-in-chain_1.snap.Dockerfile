FROM ubuntu:22.04
RUN apt-get update
SHELL ["/bin/bash", "-c"]
RUN echo done
