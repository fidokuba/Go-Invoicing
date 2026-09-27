# Invoice-template renderer service (Phases 1 and 4). Unlike the main
# API image (see the repository root Dockerfile), this one deliberately
# does ship Node and a browser engine — this is the one place in the
# system that legitimately needs both at request time (see server.js's
# own doc comment). It is not merged into the main image and the main
# image is never given Node — this stays a separate container.
#
# Build context note: this build needs both renderer/ and one file from
# web/ (see scripts/sync-template-config.mjs's own doc comment on why a
# real template's definition is rendered by importing web's own
# component config, not a hand-duplicated copy) — so compose.yaml builds
# this with the *repository root* as context, not renderer/ alone, and
# every COPY below is written relative to that root.
#
# Base image note: the root Dockerfile pins every base image by digest
# (see its own comments on why). This one is intentionally left on a
# version tag, not a digest, because verifying/pinning a real digest
# needs a working `docker pull`/`docker manifest inspect`, which wasn't
# available in the environment this was written in. Pin it to a digest
# before this goes anywhere near production, the same way every other
# image in this project is pinned.
FROM ghcr.io/puppeteer/puppeteer:25.12.0

WORKDIR /app

# The puppeteer base image's default user (pptruser) already exists and
# is non-root — reused here rather than adding a new one.
COPY --chown=pptruser:pptruser renderer/package.json ./
RUN npm install --omit=dev

COPY --chown=pptruser:pptruser renderer/scripts ./scripts
COPY --chown=pptruser:pptruser renderer/src ./src

# scripts/sync-template-config.mjs resolves its source as
# ../../web/src/components/templates/templateBuilderConfig.tsx relative
# to itself (/app/scripts here), i.e. /web/src/... — copied there, synced
# into src/generated/ once at build time (baked into the image, not
# regenerated at container start), then discarded: nothing else in this
# image ever needs web/'s source again.
COPY --chown=pptruser:pptruser web/src/components/templates/templateBuilderConfig.tsx /web/src/components/templates/templateBuilderConfig.tsx
RUN npm run sync && rm -rf /web

EXPOSE 3000
CMD ["node", "--import", "tsx/esm", "src/server.js"]
