FROM centos:7

# DL3030: Should warn - yum install without -y
RUN yum install httpd

# DL3030: Should warn - groupinstall without -y
RUN yum groupinstall "Development Tools"

# DL3030: Should warn - localinstall without -y
RUN yum localinstall package.rpm

# OK: yum update (not install)
RUN yum update

# OK: yum clean (not install)
RUN yum clean all

# OK: yum install with -y
RUN yum install -y nginx

# OK: yum with --assumeyes
RUN yum --assumeyes install vim
