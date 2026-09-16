FROM node:22-bookworm-slim AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM deps AS builder
ENV NEXT_TELEMETRY_DISABLED=1
COPY . .
RUN npm run build

FROM deps AS production-deps
RUN npm prune --omit=dev

FROM node:22-bookworm-slim AS runtime
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000
WORKDIR /app
RUN groupadd --system --gid 1001 nodejs && useradd --system --uid 1001 --gid nodejs nextjs
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
COPY --from=production-deps --chown=nextjs:nodejs /app/node_modules ./node_modules
COPY --from=builder --chown=nextjs:nodejs /app/scripts/owner.mjs ./scripts/owner.mjs
COPY --from=builder /app/LICENSE /usr/share/licenses/fact0/LICENSE
COPY --from=builder /app/fonts/dm-sans/OFL.txt /usr/share/licenses/fact0/DM-Sans-OFL.txt
COPY --from=builder /app/fonts/bricolage-grotesque/OFL.txt /usr/share/licenses/fact0/Bricolage-Grotesque-OFL.txt
COPY --from=builder /app/fonts/jetbrains-mono/OFL.txt /usr/share/licenses/fact0/JetBrains-Mono-OFL.txt
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
