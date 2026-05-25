FROM amazonlinux:2023
RUN --mount=type=cache,target=/var/cache/dnf,id=dnf,sharing=locked <<-EOF
	dnf -y update
	dnf -y install java-21-amazon-corretto-headless
EOF
