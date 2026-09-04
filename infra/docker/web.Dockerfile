FROM node:26.8.1-alpine AS build
WORKDIR /app
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY apps/web ./
RUN npm run build

FROM node:26.8.1-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app ./
EXPOSE 3000
USER node
CMD ["npm", "run", "start"]
