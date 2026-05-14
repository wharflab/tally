FROM opensuse/leap:15.4

# DL3034: Should warn - zypper install without non-interactive
RUN zypper install httpd

# DL3034: Should warn - zypper in (alias) without flag
RUN zypper in vim

# DL3034: Should warn - zypper remove without flag
RUN zypper remove nano

# DL3034: Should warn - zypper patch without flag
RUN zypper patch

# OK: zypper refresh (not install)
RUN zypper refresh

# OK: zypper clean (not install)
RUN zypper clean

# OK: zypper install with -n
RUN zypper install -n nginx

# OK: zypper install with --non-interactive
RUN zypper install --non-interactive wget

# OK: zypper install with -y
RUN zypper install -y curl

# OK: zypper install with --no-confirm
RUN zypper install --no-confirm git
