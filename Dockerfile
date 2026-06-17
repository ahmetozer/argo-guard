# Build stage
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/argo-guard ./cmd/argo-guard

# Runtime stage: the CMP sidecar
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates
COPY --from=registry.k8s.io/kustomize/kustomize:v5.4.2 /app/kustomize /usr/local/bin/kustomize
COPY --from=openpolicyagent/conftest:v0.56.0 /usr/local/bin/conftest /usr/local/bin/conftest
COPY --from=build /out/argo-guard /usr/local/bin/argo-guard
# Argo mounts argocd-cmp-server via an initContainer at /var/run/argocd.
USER 999
ENTRYPOINT ["/var/run/argocd/argocd-cmp-server"]
