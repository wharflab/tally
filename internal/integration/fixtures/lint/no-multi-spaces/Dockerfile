FROM  alpine:3.20

RUN apk add  --no-cache  curl

# This  comment  has  multiple  spaces
COPY  . /app

RUN <<EOF
echo   hello    world
EOF

CMD  ["sh"]
