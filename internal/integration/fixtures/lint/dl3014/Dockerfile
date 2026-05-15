FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7

# DL3014: Should warn - apt-get install without -y
RUN apt-get install python3

# DL3014: Should warn - multiple packages
RUN apt-get install curl wget git

# OK: apt-get update (not install)
RUN apt-get update

# OK: apt-get install with -y
RUN apt-get install -y vim

# OK: apt-get with --yes
RUN apt-get --yes install nano

# OK: apt-get with --assume-yes
RUN apt-get --assume-yes install htop

# OK: apt-get with -qq (quiet level 2 implies yes)
RUN apt-get install -qq less

# OK: apt-get with -q -q
RUN apt-get install -q -q tree
