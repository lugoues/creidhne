FROM node:20
WORKDIR /app
COPY etc/app.conf /etc/app.conf
COPY scripts/entrypoint.sh /usr/local/bin/entrypoint.sh