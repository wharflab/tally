FROM ubuntu:22.04
COPY <<JSON /etc/app/existing.json
{
  "b": 2,
  "a": 1
}
JSON

COPY <<YAML /etc/app/existing.yaml
"enabled": true
"port": 8080
YAML

ADD <<XML /etc/app/existing.xml
<app>
  <feature>on</feature>
</app>
XML

COPY <<EOF /etc/app/already.json
{
  "ok": true
}
EOF

COPY <<EOF /etc/app/config.txt
{"b":2,"a":1}
EOF

COPY <<EOF /etc/app/generated.toml
title = 'generated'

[owner]
name = 'tally'
EOF


COPY <<EOF /etc/app/php.ini
zend_extension = opcache

[opcache]
  opcache.enable = 1
EOF
