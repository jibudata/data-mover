# Build the manager binary
FROM --platform=${TARGETPLATFORM} golang:1.17 as builder
ARG TARGETARCH
ARG TARGETOS

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o manager main.go

FROM --platform=${TARGETPLATFORM} registry.cn-shanghai.aliyuncs.com/jibudata/ubi8-minimal:8.5
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
