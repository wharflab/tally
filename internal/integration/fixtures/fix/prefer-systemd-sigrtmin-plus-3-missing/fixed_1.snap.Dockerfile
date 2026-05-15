FROM fedora:40@sha256:3c86d25fef9d2001712bc3d9b091fc40cf04be4767e48f1aa3b785bf58d300ed
# [tally] SIGRTMIN+3 is the graceful shutdown signal for systemd/init
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/sbin/init"]
