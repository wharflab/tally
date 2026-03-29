FROM fedora:40
# [tally] SIGRTMIN+3 is the graceful shutdown signal for systemd/init
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/sbin/init"]
