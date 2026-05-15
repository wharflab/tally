FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
# [commented out by tally - Docker will ignore all but last CMD]: cmd echo first
CMD ["echo","second"]
ENTRYPOINT ["/bin/sh"]
