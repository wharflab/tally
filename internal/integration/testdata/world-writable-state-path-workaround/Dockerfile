# Violation: chmod 777 on state path /data
FROM ubuntu:22.04 AS stage1
RUN mkdir -p /data && chmod 777 /data

# Violation: chmod 666 on /var/lib path
FROM ubuntu:22.04 AS stage2
RUN chmod 666 /var/lib/app/config

# Violation: symbolic a+rwx
FROM ubuntu:22.04 AS stage3
RUN chmod a+rwx /app

# Violation: mkdir -m 777
FROM ubuntu:22.04 AS stage4
RUN mkdir -pm 777 /srv/logs

# Clean: chmod 755 (no world-write)
FROM ubuntu:22.04 AS clean1
RUN chmod 755 /app

# Clean: OpenShift chgrp + chmod pattern (suppressed)
FROM ubuntu:22.04 AS clean2
RUN chgrp 0 /data && chmod 777 /data

# Clean: group-only chmod g+rwx
FROM ubuntu:22.04 AS clean3
RUN chmod g+rwx /app

# Clean: chmod 770 (no world-write)
FROM ubuntu:22.04 AS clean4
RUN chmod 770 /data
