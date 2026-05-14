FROM amazonlinux:2023
RUN --mount=type=cache,target=/var/cache/dnf,id=dnf <<-EOF
	dnf -y update
	dnf clean all
	dnf -y install java-21-amazon-corretto-headless
EOF
