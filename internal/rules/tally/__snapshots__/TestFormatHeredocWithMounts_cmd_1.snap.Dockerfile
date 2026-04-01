RUN <<EOF
(curl.exe -fsSL https://example.com/app.zip -o C:\temp\app.zip && tar.exe -xf C:\temp\app.zip -C C:\tools && del C:\temp\app.zip)
EOF