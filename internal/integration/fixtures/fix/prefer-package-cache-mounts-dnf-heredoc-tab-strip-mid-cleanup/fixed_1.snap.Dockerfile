FROM amazonlinux:2023@sha256:c1872fb69ff9ed9581c999509dd0dcb4288235087f1df4999b866affdac0278d
RUN --mount=type=cache,target=/var/cache/dnf,id=dnf,sharing=locked <<-EOF
	dnf -y update
	dnf -y install java-21-amazon-corretto-headless
EOF
