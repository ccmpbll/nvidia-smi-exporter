package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reVersion = regexp.MustCompile(`(\d+\.\d+)`)
	reUnit    = regexp.MustCompile(`([\d.]+) ([KMGT]?i?)(.*)`)
	reNumber  = regexp.MustCompile(`[^0-9.]`)
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var (
	listenAddress = getEnv("EXPORTER_PORT", ":9202")
	nvidiaSMIPath = getEnv("NVIDIA_SMI_PATH", "/usr/bin/nvidia-smi")
	testMode      = os.Getenv("TEST_MODE")
)

type NvidiaSmiLog struct {
	DriverVersion string `xml:"driver_version"`
	CudaVersion   string `xml:"cuda_version"`
	AttachedGPUs  string `xml:"attached_gpus"`
	GPU           []GPU  `xml:"gpu"`
}

type GPU struct {
	Id                  string `xml:"id,attr"`
	ProductName         string `xml:"product_name"`
	ProductBrand        string `xml:"product_brand"`
	ProductArchitecture string `xml:"product_architecture"`
	UUID                string `xml:"uuid"`
	PCI                 struct {
		GPULinkInfo struct {
			PCIeGen struct {
				Max     string `xml:"max_link_gen"`
				Current string `xml:"current_link_gen"`
			} `xml:"pcie_gen"`
			LinkWidth struct {
				Max     string `xml:"max_link_width"`
				Current string `xml:"current_link_width"`
			} `xml:"link_widths"`
		} `xml:"pci_gpu_link_info"`
		ReplayCounter         string `xml:"replay_counter"`
		ReplayRolloverCounter string `xml:"replay_rollover_counter"`
		TxUtil                string `xml:"tx_util"`
		RxUtil                string `xml:"rx_util"`
	} `xml:"pci"`
	FanSpeed         string `xml:"fan_speed"`
	PerformanceState string `xml:"performance_state"`
	FbMemoryUsage    struct {
		Total string `xml:"total"`
		Used  string `xml:"used"`
		Free  string `xml:"free"`
	} `xml:"fb_memory_usage"`
	Bar1MemoryUsage struct {
		Total string `xml:"total"`
		Used  string `xml:"used"`
		Free  string `xml:"free"`
	} `xml:"bar1_memory_usage"`
	Utilization struct {
		GPUUtil     string `xml:"gpu_util"`
		MemoryUtil  string `xml:"memory_util"`
		EncoderUtil string `xml:"encoder_util"`
		DecoderUtil string `xml:"decoder_util"`
		JpegUtil    string `xml:"jpeg_util"`
		OFAUtil     string `xml:"ofa_util"`
	} `xml:"utilization"`
	EncoderStats struct {
		SessionCount   string `xml:"session_count"`
		AverageFPS     string `xml:"average_fps"`
		AverageLatency string `xml:"average_latency"`
	} `xml:"encoder_stats"`
	FBCStats struct {
		SessionCount   string `xml:"session_count"`
		AverageFPS     string `xml:"average_fps"`
		AverageLatency string `xml:"average_latency"`
	} `xml:"fbc_stats"`
	Temperature struct {
		GPUTemp                string `xml:"gpu_temp"`
		GPUTempTLimit          string `xml:"gpu_temp_tlimit"`            // Blackwell+
		GPUTempMaxThreshold    string `xml:"gpu_temp_max_threshold"`     // pre-Blackwell
		GPUTempSlowThreshold   string `xml:"gpu_temp_slow_threshold"`    // pre-Blackwell
		GPUTempMaxGpuThreshold string `xml:"gpu_temp_max_gpu_threshold"` // pre-Blackwell
		MemoryTemp             string `xml:"memory_temp"`
		GPUTempMaxMemThreshold string `xml:"gpu_temp_max_mem_threshold"` // pre-Blackwell
	} `xml:"temperature"`
	// Older nvidia-smi (pre-470): power_readings block
	PowerReadings struct {
		PowerState         string `xml:"power_state"`
		PowerDraw          string `xml:"power_draw"`
		PowerLimit         string `xml:"power_limit"`
		DefaultPowerLimit  string `xml:"default_power_limit"`
		EnforcedPowerLimit string `xml:"enforced_power_limit"`
		MinPowerLimit      string `xml:"min_power_limit"`
		MaxPowerLimit      string `xml:"max_power_limit"`
	} `xml:"power_readings"`
	// Newer nvidia-smi (470+): gpu_power_readings block
	// Blackwell (595+) renamed power_draw → average_power_draw + instant_power_draw
	GpuPowerReadings struct {
		PowerState          string `xml:"power_state"`
		PowerDraw           string `xml:"power_draw"`          // pre-Blackwell
		AveragePowerDraw    string `xml:"average_power_draw"`  // Blackwell+
		InstantPowerDraw    string `xml:"instant_power_draw"`  // Blackwell+
		CurrentPowerLimit   string `xml:"current_power_limit"`
		RequestedPowerLimit string `xml:"requested_power_limit"`
		DefaultPowerLimit   string `xml:"default_power_limit"`
		MinPowerLimit       string `xml:"min_power_limit"`
		MaxPowerLimit       string `xml:"max_power_limit"`
	} `xml:"gpu_power_readings"`
	Clocks struct {
		GraphicsClock string `xml:"graphics_clock"`
		SmClock       string `xml:"sm_clock"`
		MemClock      string `xml:"mem_clock"`
		VideoClock    string `xml:"video_clock"`
	} `xml:"clocks"`
	MaxClocks struct {
		GraphicsClock string `xml:"graphics_clock"`
		SmClock       string `xml:"sm_clock"`
		MemClock      string `xml:"mem_clock"`
		VideoClock    string `xml:"video_clock"`
	} `xml:"max_clocks"`
	ClockPolicy struct {
		AutoBoost        string `xml:"auto_boost"`
		AutoBoostDefault string `xml:"auto_boost_default"`
	} `xml:"clock_policy"`
	Processes struct {
		ProcessInfo []struct {
			Pid         string `xml:"pid"`
			Type        string `xml:"type"`
			ProcessName string `xml:"process_name"`
			UsedMemory  string `xml:"used_memory"`
		} `xml:"process_info"`
	} `xml:"processes"`
}

// isDeprecated reports whether an nvidia-smi field value indicates a feature
// removed on Blackwell architecture (driver 595+).
func isDeprecated(value string) bool {
	return strings.Contains(value, "Requested functionality has been deprecated")
}

// extractVersion returns the first major.minor version string found, or "0".
func extractVersion(value string) string {
	if m := reVersion.FindString(value); m != "" {
		return m
	}
	return "0"
}

// filterNumber strips non-numeric characters and returns the result, or "0"
// when the input is empty, "N/A", deprecated, or yields nothing after stripping.
func filterNumber(value string) string {
	if len(value) == 0 || value == "N/A" || isDeprecated(value) {
		return "0"
	}
	result := reNumber.ReplaceAllString(value, "")
	if result == "" {
		return "0"
	}
	return result
}

// filterUnit parses a value like "300 MHz" or "16311 MiB", applies SI/binary
// multipliers, and returns the result in base units. Returns "0" on failure.
func filterUnit(value string) string {
	if len(value) == 0 || value == "N/A" || isDeprecated(value) {
		return "0"
	}
	match := reUnit.FindStringSubmatch(value)
	if len(match) == 0 {
		return "0"
	}
	num, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return "0"
	}
	switch match[2] {
	case "K":
		num *= 1e3
	case "M":
		num *= 1e6
	case "G":
		num *= 1e9
	case "T":
		num *= 1e12
	case "Ki":
		num *= 1024
	case "Mi":
		num *= 1024 * 1024
	case "Gi":
		num *= 1024 * 1024 * 1024
	case "Ti":
		num *= 1024 * 1024 * 1024 * 1024
	}
	return fmt.Sprintf("%g", num)
}

// writeMetric emits a single Prometheus text-format line. It is a no-op when
// value is empty, making it structurally impossible to emit an invalid line.
func writeMetric(w io.Writer, key, labels, value string) {
	if value == "" {
		return
	}
	if labels != "" {
		fmt.Fprintf(w, "%s{%s} %s\n", key, labels, value)
	} else {
		fmt.Fprintf(w, "%s %s\n", key, value)
	}
}

func gpuLabels(gpu GPU) string {
	return fmt.Sprintf(`id="%s",uuid="%s",name="%s",architecture="%s"`,
		gpu.Id, gpu.UUID, gpu.ProductName, gpu.ProductArchitecture)
}

func metrics(w http.ResponseWriter, r *http.Request) {
	log.Print("Serving /metrics")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if testMode == "1" {
		dir, err := os.Getwd()
		if err != nil {
			log.Printf("getwd error: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		cmd = exec.CommandContext(ctx, "/bin/cat", dir+"/nvidia-smi.sample.xml")
	} else {
		cmd = exec.CommandContext(ctx, nvidiaSMIPath, "-q", "-x")
	}

	stdout, err := cmd.Output()
	if err != nil {
		log.Printf("nvidia-smi error: %v", err)
		http.Error(w, "nvidia-smi failed", http.StatusInternalServerError)
		return
	}

	var smiLog NvidiaSmiLog
	if err := xml.Unmarshal(stdout, &smiLog); err != nil {
		log.Printf("XML parse error: %v", err)
		http.Error(w, "XML parse failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	for _, gpu := range smiLog.GPU {
		labels := gpuLabels(gpu)

		writeMetric(w, "nvidiasmi_driver_version", labels, extractVersion(smiLog.DriverVersion))
		writeMetric(w, "nvidiasmi_cuda_version", labels, extractVersion(smiLog.CudaVersion))
		writeMetric(w, "nvidiasmi_attached_gpus", labels, filterNumber(smiLog.AttachedGPUs))

		writeMetric(w, "nvidiasmi_pci_pcie_gen_max", labels, filterNumber(gpu.PCI.GPULinkInfo.PCIeGen.Max))
		writeMetric(w, "nvidiasmi_pci_pcie_gen_current", labels, filterNumber(gpu.PCI.GPULinkInfo.PCIeGen.Current))
		writeMetric(w, "nvidiasmi_pci_link_width_max_multiplicator", labels, filterNumber(gpu.PCI.GPULinkInfo.LinkWidth.Max))
		writeMetric(w, "nvidiasmi_pci_link_width_current_multiplicator", labels, filterNumber(gpu.PCI.GPULinkInfo.LinkWidth.Current))
		writeMetric(w, "nvidiasmi_pci_replay_counter", labels, filterNumber(gpu.PCI.ReplayCounter))
		writeMetric(w, "nvidiasmi_pci_replay_rollover_counter", labels, filterNumber(gpu.PCI.ReplayRolloverCounter))
		writeMetric(w, "nvidiasmi_pci_tx_util_bytes_per_second", labels, filterUnit(gpu.PCI.TxUtil))
		writeMetric(w, "nvidiasmi_pci_rx_util_bytes_per_second", labels, filterUnit(gpu.PCI.RxUtil))

		writeMetric(w, "nvidiasmi_fan_speed_percent", labels, filterUnit(gpu.FanSpeed))
		writeMetric(w, "nvidiasmi_performance_state_int", labels, filterNumber(gpu.PerformanceState))

		writeMetric(w, "nvidiasmi_fb_memory_usage_total_bytes", labels, filterUnit(gpu.FbMemoryUsage.Total))
		writeMetric(w, "nvidiasmi_fb_memory_usage_used_bytes", labels, filterUnit(gpu.FbMemoryUsage.Used))
		writeMetric(w, "nvidiasmi_fb_memory_usage_free_bytes", labels, filterUnit(gpu.FbMemoryUsage.Free))
		writeMetric(w, "nvidiasmi_bar1_memory_usage_total_bytes", labels, filterUnit(gpu.Bar1MemoryUsage.Total))
		writeMetric(w, "nvidiasmi_bar1_memory_usage_used_bytes", labels, filterUnit(gpu.Bar1MemoryUsage.Used))
		writeMetric(w, "nvidiasmi_bar1_memory_usage_free_bytes", labels, filterUnit(gpu.Bar1MemoryUsage.Free))

		writeMetric(w, "nvidiasmi_utilization_gpu_percent", labels, filterUnit(gpu.Utilization.GPUUtil))
		writeMetric(w, "nvidiasmi_utilization_memory_percent", labels, filterUnit(gpu.Utilization.MemoryUtil))
		writeMetric(w, "nvidiasmi_utilization_encoder_percent", labels, filterUnit(gpu.Utilization.EncoderUtil))
		writeMetric(w, "nvidiasmi_utilization_decoder_percent", labels, filterUnit(gpu.Utilization.DecoderUtil))
		writeMetric(w, "nvidiasmi_utilization_jpeg_percent", labels, filterUnit(gpu.Utilization.JpegUtil))
		writeMetric(w, "nvidiasmi_utilization_ofa_percent", labels, filterUnit(gpu.Utilization.OFAUtil))

		writeMetric(w, "nvidiasmi_encoder_session_count", labels, filterNumber(gpu.EncoderStats.SessionCount))
		writeMetric(w, "nvidiasmi_encoder_average_fps", labels, filterNumber(gpu.EncoderStats.AverageFPS))
		writeMetric(w, "nvidiasmi_encoder_average_latency", labels, filterNumber(gpu.EncoderStats.AverageLatency))
		writeMetric(w, "nvidiasmi_fbc_session_count", labels, filterNumber(gpu.FBCStats.SessionCount))
		writeMetric(w, "nvidiasmi_fbc_average_fps", labels, filterNumber(gpu.FBCStats.AverageFPS))
		writeMetric(w, "nvidiasmi_fbc_average_latency", labels, filterNumber(gpu.FBCStats.AverageLatency))

		writeMetric(w, "nvidiasmi_gpu_temp_celsius", labels, filterUnit(gpu.Temperature.GPUTemp))
		writeMetric(w, "nvidiasmi_gpu_temp_tlimit_celsius", labels, filterUnit(gpu.Temperature.GPUTempTLimit))
		writeMetric(w, "nvidiasmi_gpu_temp_max_threshold_celsius", labels, filterUnit(gpu.Temperature.GPUTempMaxThreshold))
		writeMetric(w, "nvidiasmi_gpu_temp_slow_threshold_celsius", labels, filterUnit(gpu.Temperature.GPUTempSlowThreshold))
		writeMetric(w, "nvidiasmi_gpu_temp_max_gpu_threshold_celsius", labels, filterUnit(gpu.Temperature.GPUTempMaxGpuThreshold))
		writeMetric(w, "nvidiasmi_memory_temp_celsius", labels, filterUnit(gpu.Temperature.MemoryTemp))
		writeMetric(w, "nvidiasmi_gpu_temp_max_mem_threshold_celsius", labels, filterUnit(gpu.Temperature.GPUTempMaxMemThreshold))

		emitPowerMetrics(w, gpu, labels)

		writeMetric(w, "nvidiasmi_clock_graphics_hertz", labels, filterUnit(gpu.Clocks.GraphicsClock))
		writeMetric(w, "nvidiasmi_clock_graphics_max_hertz", labels, filterUnit(gpu.MaxClocks.GraphicsClock))
		writeMetric(w, "nvidiasmi_clock_sm_hertz", labels, filterUnit(gpu.Clocks.SmClock))
		writeMetric(w, "nvidiasmi_clock_sm_max_hertz", labels, filterUnit(gpu.MaxClocks.SmClock))
		writeMetric(w, "nvidiasmi_clock_mem_hertz", labels, filterUnit(gpu.Clocks.MemClock))
		writeMetric(w, "nvidiasmi_clock_mem_max_hertz", labels, filterUnit(gpu.MaxClocks.MemClock))
		writeMetric(w, "nvidiasmi_clock_video_hertz", labels, filterUnit(gpu.Clocks.VideoClock))
		writeMetric(w, "nvidiasmi_clock_video_max_hertz", labels, filterUnit(gpu.MaxClocks.VideoClock))
		writeMetric(w, "nvidiasmi_clock_policy_auto_boost", labels, filterUnit(gpu.ClockPolicy.AutoBoost))
		writeMetric(w, "nvidiasmi_clock_policy_auto_boost_default", labels, filterUnit(gpu.ClockPolicy.AutoBoostDefault))

		for _, proc := range gpu.Processes.ProcessInfo {
			if proc.UsedMemory == "" || proc.UsedMemory == "N/A" || isDeprecated(proc.UsedMemory) {
				continue
			}
			procLabels := fmt.Sprintf(`%s,process_pid="%s",process_name="%s",process_type="%s"`,
				labels, proc.Pid, proc.ProcessName, proc.Type)
			writeMetric(w, "nvidiasmi_process_used_memory_bytes", procLabels, filterUnit(proc.UsedMemory))
		}
	}
}

// emitPowerMetrics handles the three different power block shapes across
// nvidia-smi generations:
//
//   - power_readings (pre-470): power_draw, power_limit
//   - gpu_power_readings pre-Blackwell (470–594): power_draw, current_power_limit
//   - gpu_power_readings Blackwell (595+): average_power_draw, instant_power_draw,
//     current_power_limit — power_state returns the deprecated string and is skipped
//
// Power-state metrics (nvidiasmi_power_state_int / nvidiasmi_gpu_power_state_int)
// are emitted only when the source value is a valid P-state; they are silently
// omitted on Blackwell where the field is deprecated.
func emitPowerMetrics(w io.Writer, gpu GPU, labels string) {
	if gpu.GpuPowerReadings.CurrentPowerLimit != "" {
		// gpu_power_readings block present (driver 470+)
		if !isDeprecated(gpu.GpuPowerReadings.PowerState) && gpu.GpuPowerReadings.PowerState != "" {
			writeMetric(w, "nvidiasmi_power_state_int", labels, filterNumber(gpu.GpuPowerReadings.PowerState))
			writeMetric(w, "nvidiasmi_gpu_power_state_int", labels, filterNumber(gpu.GpuPowerReadings.PowerState))
		}

		if gpu.GpuPowerReadings.AveragePowerDraw != "" {
			// Blackwell (595+): split into average + instant
			writeMetric(w, "nvidiasmi_power_draw_watts", labels, filterUnit(gpu.GpuPowerReadings.AveragePowerDraw))
			writeMetric(w, "nvidiasmi_gpu_average_power_draw_watts", labels, filterUnit(gpu.GpuPowerReadings.AveragePowerDraw))
			writeMetric(w, "nvidiasmi_gpu_instant_power_draw_watts", labels, filterUnit(gpu.GpuPowerReadings.InstantPowerDraw))
		} else if gpu.GpuPowerReadings.PowerDraw != "" {
			// Pre-Blackwell gpu_power_readings
			writeMetric(w, "nvidiasmi_power_draw_watts", labels, filterUnit(gpu.GpuPowerReadings.PowerDraw))
		}

		writeMetric(w, "nvidiasmi_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.CurrentPowerLimit))
		writeMetric(w, "nvidiasmi_default_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.DefaultPowerLimit))
		writeMetric(w, "nvidiasmi_enforced_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.RequestedPowerLimit))
		writeMetric(w, "nvidiasmi_min_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.MinPowerLimit))
		writeMetric(w, "nvidiasmi_max_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.MaxPowerLimit))
		writeMetric(w, "nvidiasmi_gpu_current_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.CurrentPowerLimit))
		writeMetric(w, "nvidiasmi_gpu_requested_power_limit", labels, filterUnit(gpu.GpuPowerReadings.RequestedPowerLimit))
		writeMetric(w, "nvidiasmi_gpu_default_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.DefaultPowerLimit))
		writeMetric(w, "nvidiasmi_gpu_min_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.MinPowerLimit))
		writeMetric(w, "nvidiasmi_gpu_max_power_limit_watts", labels, filterUnit(gpu.GpuPowerReadings.MaxPowerLimit))
	} else if gpu.PowerReadings.PowerDraw != "" || gpu.PowerReadings.PowerLimit != "" {
		// Legacy power_readings block (pre-470)
		if !isDeprecated(gpu.PowerReadings.PowerState) && gpu.PowerReadings.PowerState != "" {
			writeMetric(w, "nvidiasmi_power_state_int", labels, filterNumber(gpu.PowerReadings.PowerState))
		}
		writeMetric(w, "nvidiasmi_power_draw_watts", labels, filterUnit(gpu.PowerReadings.PowerDraw))
		writeMetric(w, "nvidiasmi_power_limit_watts", labels, filterUnit(gpu.PowerReadings.PowerLimit))
		writeMetric(w, "nvidiasmi_default_power_limit_watts", labels, filterUnit(gpu.PowerReadings.DefaultPowerLimit))
		writeMetric(w, "nvidiasmi_enforced_power_limit_watts", labels, filterUnit(gpu.PowerReadings.EnforcedPowerLimit))
		writeMetric(w, "nvidiasmi_min_power_limit_watts", labels, filterUnit(gpu.PowerReadings.MinPowerLimit))
		writeMetric(w, "nvidiasmi_max_power_limit_watts", labels, filterUnit(gpu.PowerReadings.MaxPowerLimit))
	}
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "ok")
}

func index(w http.ResponseWriter, r *http.Request) {
	log.Print("Serving /index")
	io.WriteString(w, `<!doctype html>
<html>
    <head><meta charset="utf-8"><title>Nvidia SMI Exporter</title></head>
    <body>
        <h1>Nvidia SMI Exporter</h1>
        <p><a href="/metrics">Metrics</a></p>
        <p><a href="/healthz">Health</a></p>
    </body>
</html>`)
}

func logDetectedGPUs() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if testMode == "1" {
		dir, _ := os.Getwd()
		cmd = exec.CommandContext(ctx, "/bin/cat", dir+"/nvidia-smi.sample.xml")
	} else {
		cmd = exec.CommandContext(ctx, nvidiaSMIPath, "-q", "-x")
	}

	stdout, err := cmd.Output()
	if err != nil {
		log.Printf("GPU detection failed: %v", err)
		return
	}

	var smiLog NvidiaSmiLog
	if err := xml.Unmarshal(stdout, &smiLog); err != nil {
		log.Printf("GPU detection XML parse error: %v", err)
		return
	}

	log.Printf("Detected %s GPU(s)", smiLog.AttachedGPUs)
	for i, gpu := range smiLog.GPU {
		log.Printf("  GPU %d: %s | %s | %s", i, gpu.ProductName, gpu.ProductArchitecture, gpu.UUID)
	}
}

func main() {
	if testMode == "1" {
		log.Print("Test mode is enabled")
	}
	logDetectedGPUs()
	log.Printf("Nvidia SMI exporter listening on %s", listenAddress)
	http.HandleFunc("/", index)
	http.HandleFunc("/healthz", healthz)
	http.HandleFunc("/metrics", metrics)
	log.Fatal(http.ListenAndServe(listenAddress, nil))
}
