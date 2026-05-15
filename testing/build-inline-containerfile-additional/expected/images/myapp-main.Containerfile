FROM node:20
WORKDIR /app
COPY . .
RUN npm ci && npm run build