FROM node:22-alpine AS build
WORKDIR /src
# 同 server/Dockerfile：默认走国内 npm 镜像，版本由 package-lock.json 钉死。
RUN npm config set registry https://registry.npmmirror.com
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
# 契约类型（src/api/schema.d.ts）是入库的生成物，构建时不重新生成；build 已含 tsc 类型检查。
RUN npm run build

FROM caddy:2-alpine
COPY Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/dist /srv
EXPOSE 80
