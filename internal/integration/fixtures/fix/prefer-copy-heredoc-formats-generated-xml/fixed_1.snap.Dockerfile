FROM ubuntu:22.04

COPY <<EOF /etc/app/config.xml
<root>
  <child>1</child>
</root>
EOF
