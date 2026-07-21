# Build stage: runs on the build host's arch, cross-compiles for the target.
FROM --platform=$BUILDPLATFORM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/argo-guard ./cmd/argo-guard

# The kustomize image is amd64-only, so fetch the release binary per arch.
FROM --platform=$BUILDPLATFORM alpine:3.20 AS kustomize
ARG TARGETOS TARGETARCH
RUN wget -qO- "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv5.4.2/kustomize_v5.4.2_${TARGETOS}_${TARGETARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin kustomize

# Runtime stage: the CMP sidecar
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
COPY --from=kustomize /usr/local/bin/kustomize /usr/local/bin/kustomize
COPY --from=openpolicyagent/conftest:v0.56.0 /usr/local/bin/conftest /usr/local/bin/conftest
COPY --from=build /out/argo-guard /usr/local/bin/argo-guard
# Argo mounts argocd-cmp-server via an initContainer at /var/run/argocd.
USER 999
ENTRYPOINT ["/var/run/argocd/argocd-cmp-server"]
