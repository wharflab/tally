FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7
COPY <<JSON /etc/app/existing.json
{"b":2,"a":1}
JSON

COPY <<YAML /etc/app/existing.yaml
{"enabled":true,"port":8080}
YAML

ADD <<XML /etc/app/existing.xml
<app><feature>on</feature></app>
XML

COPY <<EOF /etc/app/already.json
{
  "ok": true
}
EOF

COPY <<EOF /etc/app/config.txt
{"b":2,"a":1}
EOF

RUN printf 'title="generated"\n[owner]\nname="tally"\n' > /etc/app/generated.toml
RUN printf 'zend_extension=opcache\n[opcache]\nopcache.enable=1\n' > /etc/app/php.ini
