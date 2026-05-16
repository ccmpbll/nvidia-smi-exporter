# nvidia-smi-exporter

![Image Build Status](https://img.shields.io/github/actions/workflow/status/ccmpbll/nvidia-smi-exporter/docker.yml) ![Docker Image Size](https://img.shields.io/docker/image-size/ccmpbll/nvidia-smi-exporter/latest) ![Docker Pulls](https://img.shields.io/docker/pulls/ccmpbll/nvidia-smi-exporter.svg) ![License](https://img.shields.io/badge/License-MIT-blue.svg)

A Prometheus exporter for NVIDIA GPU metrics via `nvidia-smi`. Compatible with all GPU
architectures (Turing, Ampere, Ada Lovelace, Blackwell) and **all driver versions**, including
driver 595+ which deprecated several fields across all architectures.

## Why this project exists

The original exporter (and its forks) fail on driver 595+. Starting with driver 595, NVIDIA
changed several `nvidia-smi -q -x` fields from `N/A` to the string `"Requested functionality
has been deprecated"` — on **all GPU architectures**, not just Blackwell. The original
`filterNumber()` function strips non-numeric characters from this string, producing an empty
string, which is then emitted as a Prometheus metric value — invalid text exposition format
that causes the **entire scrape to fail silently**, dropping all metrics for that interval.

This rewrite fixes that at the structural level: every value passes through a safe filter
that always returns a valid numeric string or `"0"`, and `writeMetric` refuses to emit any
line with an empty value.

## Credits

This project is derived from three MIT-licensed upstream projects:

- [kristophjunge/docker-prometheus-nvidiasmi](https://github.com/kristophjunge/docker-prometheus-nvidiasmi) — original exporter
- [e7d/docker-prometheus-nvidiasmi](https://github.com/e7d/docker-prometheus-nvidiasmi) — fork adding `gpu_power_readings` support
- [ich777/unraid-prometheus_nvidia_smi_exporter](https://github.com/ich777/unraid-prometheus_nvidia_smi_exporter) — Unraid packaging

See [LICENSE](LICENSE) for full attribution.

## Docker Usage

**Requirements:** NVIDIA Container Toolkit installed on the host.

### docker-compose

```yaml
services:
  nvidia-smi-exporter:
    image: ccmpbll/nvidia-smi-exporter:latest
    runtime: nvidia
    ports:
      - "9202:9202/tcp"
    environment:
      - NVIDIA_VISIBLE_DEVICES=all
    restart: unless-stopped
```

The container image includes `nvidia-smi` from the CUDA base image — no host bind-mount
is needed. The NVIDIA runtime provides GPU device access at runtime.

### docker run

```bash
docker run -d \
  --runtime=nvidia \
  -p 9202:9202 \
  -e NVIDIA_VISIBLE_DEVICES=all \
  ccmpbll/nvidia-smi-exporter:latest
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `EXPORTER_PORT` | `:9202` | HTTP listen address |
| `NVIDIA_VISIBLE_DEVICES` | — | GPUs to expose: `all` (all GPUs), `0` / `0,1` (by index), `GPU-<uuid>` (by UUID), `none` (disable GPU access) |

## Unraid Installation

### Prerequisites

Install the **Nvidia-Driver** plugin from Community Applications first.
This plugin installs `nvidia-smi` at `/usr/bin/nvidia-smi` on the Unraid host.

The nvidia-smi-exporter plugin will abort installation with a clear error message if
`/usr/bin/nvidia-smi` is not found.

### Install via Community Applications

Search for "nvidia-smi-exporter" in the Community Applications store, or add the plugin
URL directly in the Unraid Plugins tab:

```
https://github.com/ccmpbll/nvidia-smi-exporter/raw/main/unraid/nvidia-smi-exporter.plg
```

### Configuration

After installation, navigate to **Settings → Nvidia SMI Exporter** in the Unraid web UI to:
- Start / stop / restart the exporter
- Configure the listen port (default: 9202)
- View current GPU information

Settings are persisted to `/boot/config/plugins/nvidia-smi-exporter/nvidia-smi-exporter.cfg`.

## Prometheus Scrape Configuration

```yaml
scrape_configs:
  - job_name: nvidia_smi
    static_configs:
      - targets:
          - 'your-host:9202'
    scrape_interval: 15s
```

## Metrics Reference

All metrics carry these labels:

| Label | Example |
|---|---|
| `id` | `00000000:05:00.0` |
| `uuid` | `GPU-a3d4d0e5-dec9-bb01-0332-cc270b0e8793` |
| `name` | `NVIDIA GeForce RTX 5060 Ti` |
| `architecture` | `Blackwell` |

`nvidiasmi_process_used_memory_bytes` additionally carries:
`process_pid`, `process_name`, `process_type`.

### System

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_driver_version` | — | Driver major.minor version as float |
| `nvidiasmi_cuda_version` | — | CUDA major.minor version as float |
| `nvidiasmi_attached_gpus` | — | Total number of attached GPUs |

### PCIe

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_pci_pcie_gen_max` | — | Maximum PCIe generation |
| `nvidiasmi_pci_pcie_gen_current` | — | Current PCIe generation |
| `nvidiasmi_pci_link_width_max_multiplicator` | — | Maximum PCIe link width (e.g. 16 for 16x) |
| `nvidiasmi_pci_link_width_current_multiplicator` | — | Current PCIe link width |
| `nvidiasmi_pci_replay_counter` | — | PCIe replay counter |
| `nvidiasmi_pci_replay_rollover_counter` | — | PCIe replay rollover counter |
| `nvidiasmi_pci_tx_util_bytes_per_second` | bytes/s | PCIe TX utilization |
| `nvidiasmi_pci_rx_util_bytes_per_second` | bytes/s | PCIe RX utilization |

### GPU State

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_fan_speed_percent` | % | Fan speed |
| `nvidiasmi_performance_state_int` | — | P-state number (e.g. 8 for P8) |

### Memory

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_fb_memory_usage_total_bytes` | bytes | Framebuffer total |
| `nvidiasmi_fb_memory_usage_used_bytes` | bytes | Framebuffer used |
| `nvidiasmi_fb_memory_usage_free_bytes` | bytes | Framebuffer free |
| `nvidiasmi_bar1_memory_usage_total_bytes` | bytes | BAR1 memory total |
| `nvidiasmi_bar1_memory_usage_used_bytes` | bytes | BAR1 memory used |
| `nvidiasmi_bar1_memory_usage_free_bytes` | bytes | BAR1 memory free |

### Utilization

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_utilization_gpu_percent` | % | GPU core utilization |
| `nvidiasmi_utilization_memory_percent` | % | Memory controller utilization |
| `nvidiasmi_utilization_encoder_percent` | % | Video encoder utilization |
| `nvidiasmi_utilization_decoder_percent` | % | Video decoder utilization |
| `nvidiasmi_utilization_jpeg_percent` | % | JPEG engine utilization (Turing+) |
| `nvidiasmi_utilization_ofa_percent` | % | Optical flow accelerator utilization (Turing+) |

### Encoder / FBC Stats

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_encoder_session_count` | — | Active encoder sessions |
| `nvidiasmi_encoder_average_fps` | — | Average encoder FPS |
| `nvidiasmi_encoder_average_latency` | — | Average encoder latency |
| `nvidiasmi_fbc_session_count` | — | Active frame buffer capture sessions |
| `nvidiasmi_fbc_average_fps` | — | Average FBC FPS |
| `nvidiasmi_fbc_average_latency` | — | Average FBC latency |

### Temperature

| Metric | Unit | Notes |
|---|---|---|
| `nvidiasmi_gpu_temp_celsius` | °C | Current GPU temperature |
| `nvidiasmi_gpu_temp_tlimit_celsius` | °C | Thermal limit (Blackwell+); 0 on older GPUs |
| `nvidiasmi_gpu_temp_max_threshold_celsius` | °C | Max threshold (pre-Blackwell); 0 on Blackwell |
| `nvidiasmi_gpu_temp_slow_threshold_celsius` | °C | Slow threshold (pre-Blackwell); 0 on Blackwell |
| `nvidiasmi_gpu_temp_max_gpu_threshold_celsius` | °C | Max GPU threshold (pre-Blackwell); 0 on Blackwell |
| `nvidiasmi_memory_temp_celsius` | °C | Memory temperature; 0 if N/A |
| `nvidiasmi_gpu_temp_max_mem_threshold_celsius` | °C | Max memory threshold (pre-Blackwell); 0 on Blackwell |

### Power

| Metric | Unit | Notes |
|---|---|---|
| `nvidiasmi_power_state_int` | — | P-state number; **omitted on driver 595+** (deprecated field) |
| `nvidiasmi_gpu_power_state_int` | — | P-state number (gpu_power_readings); **omitted on driver 595+** |
| `nvidiasmi_power_draw_watts` | W | Compat alias: average_power_draw (Blackwell) or power_draw |
| `nvidiasmi_power_limit_watts` | W | Current power limit |
| `nvidiasmi_default_power_limit_watts` | W | Default power limit |
| `nvidiasmi_enforced_power_limit_watts` | W | Enforced / requested power limit |
| `nvidiasmi_min_power_limit_watts` | W | Minimum configurable power limit |
| `nvidiasmi_max_power_limit_watts` | W | Maximum configurable power limit |
| `nvidiasmi_gpu_average_power_draw_watts` | W | Average power draw (Blackwell+) |
| `nvidiasmi_gpu_instant_power_draw_watts` | W | Instantaneous power draw (Blackwell+) |
| `nvidiasmi_gpu_current_power_limit_watts` | W | Current power limit (gpu_power_readings) |
| `nvidiasmi_gpu_requested_power_limit` | W | Requested power limit |
| `nvidiasmi_gpu_default_power_limit_watts` | W | Default power limit (gpu_power_readings) |
| `nvidiasmi_gpu_min_power_limit_watts` | W | Min power limit (gpu_power_readings) |
| `nvidiasmi_gpu_max_power_limit_watts` | W | Max power limit (gpu_power_readings) |

### Clocks

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_clock_graphics_hertz` | Hz | Current graphics clock |
| `nvidiasmi_clock_graphics_max_hertz` | Hz | Maximum graphics clock |
| `nvidiasmi_clock_sm_hertz` | Hz | Current SM clock |
| `nvidiasmi_clock_sm_max_hertz` | Hz | Maximum SM clock |
| `nvidiasmi_clock_mem_hertz` | Hz | Current memory clock |
| `nvidiasmi_clock_mem_max_hertz` | Hz | Maximum memory clock |
| `nvidiasmi_clock_video_hertz` | Hz | Current video clock |
| `nvidiasmi_clock_video_max_hertz` | Hz | Maximum video clock |
| `nvidiasmi_clock_policy_auto_boost` | — | Auto-boost policy state; 0 if N/A |
| `nvidiasmi_clock_policy_auto_boost_default` | — | Auto-boost default state; 0 if N/A |

### Processes

| Metric | Unit | Description |
|---|---|---|
| `nvidiasmi_process_used_memory_bytes` | bytes | GPU memory used by process |

Additional labels: `process_pid`, `process_name`, `process_type` (`C`=compute, `G`=graphics).

## Known Limitations

### Driver 595+ (all architectures)

Starting with driver 595, several nvidia-smi fields return `"Requested functionality has been
deprecated"` rather than a numeric value or `N/A`, on **all GPU architectures**. This exporter
handles them as follows:

| Field | Behavior |
|---|---|
| `power_state` in `gpu_power_readings` | `nvidiasmi_power_state_int` and `nvidiasmi_gpu_power_state_int` are **not emitted** |
| `power_draw` in `gpu_power_readings` | Replaced by `average_power_draw` + `instant_power_draw` |
| `applications_clocks.*` | Not parsed; no metric emitted |
| `display_mode` | Not exposed as a metric in any version |
| Old temperature threshold field names | Fields were renamed; old fields return 0 on Blackwell |

### P-state metrics

`nvidiasmi_power_state_int` and `nvidiasmi_gpu_power_state_int` are emitted only when
the source value is a valid P-state string (e.g. `P0`, `P8`). They are silently omitted
on driver 595+ where the field is deprecated. Pre-existing dashboards that graph these metrics
will show gaps on affected GPUs rather than incorrect zero values.

## Building from Source

```bash
# Docker image
docker build -f docker/Dockerfile -t nvidia-smi-exporter .

# Native binary (requires Go 1.22+)
cd src && go build -o nvidia-smi-exporter .

# Test with sample XML
TEST_MODE=1 ./nvidia-smi-exporter
curl -s http://localhost:9202/metrics

# Unraid package
./unraid/build.sh 2026.05.12
```

## Contributing

Issues and PRs welcome. When reporting a metrics bug, please include the output of
`nvidia-smi -q -x` from your system (you can redact UUIDs and serial numbers).
