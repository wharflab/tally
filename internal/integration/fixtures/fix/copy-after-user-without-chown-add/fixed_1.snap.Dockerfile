FROM ubuntu:22.04
USER 1000
ADD --chown=1000 config.tar.gz /etc/app/
RUN setup.sh
