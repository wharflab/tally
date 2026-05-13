FROM fedora:38

# DL3038: Should warn - dnf install without -y
RUN dnf install httpd

# DL3038: Should warn - microdnf install without -y
RUN microdnf install nginx

# DL3038: Should warn - groupinstall without -y
RUN dnf groupinstall "Development Tools"

# DL3038: Should warn - localinstall without -y
RUN dnf localinstall package.rpm

# OK: dnf update (not install)
RUN dnf update

# OK: dnf clean (not install)
RUN dnf clean all

# OK: dnf install with -y
RUN dnf install -y vim

# OK: microdnf install with -y
RUN microdnf install -y curl

# OK: dnf with --assumeyes
RUN dnf --assumeyes install wget

# OK: similar name but not dnf
RUN notdnf install httpd
