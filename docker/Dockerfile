# Stage 1: build a fully static binary
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY src/ .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o nvidia-smi-exporter .

# Stage 2: minimal CUDA image that ships nvidia-smi
FROM nvidia/cuda:12.6.2-base-ubuntu24.04

RUN useradd -r -s /usr/sbin/nologin nvidia-smi-exporter

COPY --from=builder /build/nvidia-smi-exporter /usr/local/bin/nvidia-smi-exporter

# utility capability is the minimal set needed for nvidia-smi
ENV NVIDIA_DRIVER_CAPABILITIES=utility

EXPOSE 9202

USER nvidia-smi-exporter

ENTRYPOINT ["/usr/local/bin/nvidia-smi-exporter"]
