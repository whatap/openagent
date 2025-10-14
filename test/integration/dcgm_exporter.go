package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"open-agent/pkg/model"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
)

// DCGMData represents a single row from the DCGM CSV
type DCGMData struct {
	GPU                      string
	UUID                     string
	PCIBusID                 string
	Device                   string
	ModelName                string
	Hostname                 string
	DCGMFIDevComputeMode     string
	DCGMFIDevMigCIInfo       string
	DCGMFIDevMigGIInfo       string
	DCGMFIDevMigMaxSlices    string
	DCGMFIDevMigMode         string
	DCGMFIDevName            string
	DCGMFIDevPersistenceMode string
	DCGMFIDevSerial          string
	DCGMFIDevUUID            string
	DCGMFIDevVirtualMode     string
	DCGMFIDriverVersion      string
	DCGMFINvmlVersion        string
	CalculationMethod        string
	Container                string
	Namespace                string
	Pod                      string
	WtpSrc                   string
	Instance                 string
	Node                     string
	Value                    string
	Time                     string
	UsagePattern             string // Will be assigned based on device index
}

// readDCGMData reads the DCGM CSV file and returns the data
func readDCGMData(filename string) ([]DCGMData, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %v", err)
	}

	var data []DCGMData
	// Skip header row
	for i := 1; i < len(records); i++ {
		if len(records[i]) >= 27 {
			// Assign usage pattern based on device index within each node
			deviceIdx, _ := strconv.Atoi(records[i][0]) // GPU column
			var usagePattern string

			// Pattern distribution: 3×A (devices 0,1,2), 3×B (devices 3,4,5), 2×low (devices 6,7)
			switch deviceIdx {
			case 0, 1, 2:
				usagePattern = "A" // High usage Mon-Tue
			case 3, 4, 5:
				usagePattern = "B" // High usage Wed-Thu-Fri
			case 6, 7:
				usagePattern = "low" // Consistently low usage
			default:
				usagePattern = "low"
			}

			data = append(data, DCGMData{
				GPU:                      records[i][0],
				UUID:                     records[i][1],
				PCIBusID:                 records[i][2],
				Device:                   records[i][3],
				ModelName:                records[i][4],
				Hostname:                 records[i][5],
				DCGMFIDevComputeMode:     records[i][6],
				DCGMFIDevMigCIInfo:       records[i][7],
				DCGMFIDevMigGIInfo:       records[i][8],
				DCGMFIDevMigMaxSlices:    records[i][9],
				DCGMFIDevMigMode:         records[i][10],
				DCGMFIDevName:            records[i][11],
				DCGMFIDevPersistenceMode: records[i][12],
				DCGMFIDevSerial:          records[i][13],
				DCGMFIDevUUID:            records[i][14],
				DCGMFIDevVirtualMode:     records[i][15],
				DCGMFIDriverVersion:      records[i][16],
				DCGMFINvmlVersion:        records[i][17],
				CalculationMethod:        records[i][18],
				Container:                records[i][19],
				Namespace:                records[i][20],
				Pod:                      records[i][21],
				WtpSrc:                   records[i][22],
				Instance:                 records[i][23],
				Node:                     records[i][24],
				Value:                    records[i][25],
				Time:                     records[i][26],
				UsagePattern:             usagePattern,
			})
		}
	}

	return data, nil
}

// calculateUtilizationValue generates utilization based on usage pattern and day of week
func calculateUtilizationValue(pattern string, weekday time.Weekday, currentTime time.Time) float64 {
	hour := currentTime.Hour()

	// Create daily variation factor based on date to ensure consistent daily patterns
	// Use year, month, day to create a seed that's consistent for the same date
	dayOfYear := currentTime.YearDay()
	dailyVariation := float64((dayOfYear*13+currentTime.Day()*7)%100) / 100.0 // 0.0 to 0.99

	// Daily variation modifier: -10% to +10% of the base range
	dailyModifier := (dailyVariation - 0.5) * 0.2 // -0.1 to +0.1

	switch pattern {
	case "A": // High usage on Monday-Tuesday
		if weekday == time.Monday || weekday == time.Tuesday {
			// High usage during business hours, lower at night
			if hour >= 9 && hour <= 17 {
				baseValue := 0.6 + rand.Float64()*0.35 // 0.60-0.95
				// Apply daily modifier, ensuring we stay within reasonable bounds
				return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier))
			} else {
				baseValue := 0.2 + rand.Float64()*0.4 // 0.20-0.60
				return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier))
			}
		} else {
			// Low usage on other days
			baseValue := rand.Float64() * 0.3                                // 0.00-0.30
			return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier*0.5)) // Smaller modifier for low usage days
		}

	case "B": // High usage on Wednesday-Thursday-Friday
		if weekday == time.Wednesday || weekday == time.Thursday || weekday == time.Friday {
			// High usage during business hours, lower at night
			if hour >= 9 && hour <= 17 {
				baseValue := 0.65 + rand.Float64()*0.3 // 0.65-0.95
				return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier))
			} else {
				baseValue := 0.25 + rand.Float64()*0.4 // 0.25-0.65
				return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier))
			}
		} else {
			// Low usage on other days
			baseValue := rand.Float64() * 0.25                               // 0.00-0.25
			return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier*0.5)) // Smaller modifier for low usage days
		}

	case "low": // Consistently low usage
		baseValue := rand.Float64() * 0.2                                // 0.00-0.20
		return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier*0.3)) // Small modifier for low usage pattern

	default:
		baseValue := rand.Float64() // 0.00-1.00
		return math.Max(0.0, math.Min(1.0, baseValue+dailyModifier))
	}
}

// createDCGMMetricsFromCSV creates OpenMx metrics from DCGM data
func createDCGMMetricsFromCSV(data []DCGMData, timestamp time.Time) []*model.OpenMx {
	metrics := make([]*model.OpenMx, 0, len(data))
	weekday := timestamp.Weekday()

	for _, device := range data {
		// Calculate utilization based on usage pattern and day of week
		utilization := calculateUtilizationValue(device.UsagePattern, weekday, timestamp)

		// Create OpenMx metric
		mx := model.NewOpenMx("DCGM_FI_DEV_WEIGHTED_GPU_UTIL", timestamp.UnixMilli(), utilization)

		// Add labels based on DCGM data structure
		mx.AddLabel("gpu", device.GPU)
		mx.AddLabel("UUID", device.UUID)
		mx.AddLabel("pci_bus_id", device.PCIBusID)
		mx.AddLabel("device", device.Device)
		mx.AddLabel("modelName", device.ModelName)
		mx.AddLabel("Hostname", device.Hostname)
		mx.AddLabel("DCGM_FI_DEV_COMPUTE_MODE", device.DCGMFIDevComputeMode)
		mx.AddLabel("DCGM_FI_DEV_MIG_CI_INFO", device.DCGMFIDevMigCIInfo)
		mx.AddLabel("DCGM_FI_DEV_MIG_GI_INFO", device.DCGMFIDevMigGIInfo)
		mx.AddLabel("DCGM_FI_DEV_MIG_MAX_SLICES", device.DCGMFIDevMigMaxSlices)
		mx.AddLabel("DCGM_FI_DEV_MIG_MODE", device.DCGMFIDevMigMode)
		mx.AddLabel("DCGM_FI_DEV_NAME", device.DCGMFIDevName)
		mx.AddLabel("DCGM_FI_DEV_PERSISTENCE_MODE", device.DCGMFIDevPersistenceMode)
		mx.AddLabel("DCGM_FI_DEV_SERIAL", device.DCGMFIDevSerial)
		mx.AddLabel("DCGM_FI_DEV_UUID", device.DCGMFIDevUUID)
		mx.AddLabel("DCGM_FI_DEV_VIRTUAL_MODE", device.DCGMFIDevVirtualMode)
		mx.AddLabel("DCGM_FI_DRIVER_VERSION", device.DCGMFIDriverVersion)
		mx.AddLabel("DCGM_FI_NVML_VERSION", device.DCGMFINvmlVersion)
		mx.AddLabel("calculation_method", device.CalculationMethod)
		mx.AddLabel("container", device.Container)
		mx.AddLabel("namespace", device.Namespace)
		mx.AddLabel("pod", device.Pod)
		mx.AddLabel("wtp_src", device.WtpSrc)
		mx.AddLabel("instance", device.Instance)
		mx.AddLabel("node", device.Node)

		metrics = append(metrics, mx)
	}

	return metrics
}

// New structs for raw metrics (non-weighted)
type DCGMUtilRow struct {
	GPU              string
	UUID             string
	PCIBusID         string
	Device           string
	ModelName        string
	Hostname         string
	DCGMFIDevMigMode string
	DCGMFIDevName    string
	DCGMFIDevUUID    string
	DCGMFIDriverVer  string
	DCGMFINvmlVer    string
	Container        string
	Namespace        string
	Pod              string
	WtpSrc           string
	Instance         string
	Node             string
	Time             string
	UsagePattern     string
}

type GRActiveRow struct {
	GPU                   string
	UUID                  string
	PCIBusID              string
	Device                string
	ModelName             string
	GPU_I_PROFILE         string
	GPU_I_ID              string
	Hostname              string
	DCGMFIDevMigMaxSlices string
	DCGMFIDevMigMode      string
	DCGMFIDevName         string
	DCGMFIDevUUID         string
	DCGMFIDriverVer       string
	DCGMFINvmlVer         string
	Container             string
	Namespace             string
	Pod                   string
	WtpSrc                string
	Instance              string
	Node                  string
	Time                  string
	UsagePattern          string
}

// Helper to map header to index
func headerIndexMap(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[h] = i
	}
	return m
}

// UTF-8 BOM-safe CSV reader with relaxed parsing options
func newCSVReader(r io.Reader) *csv.Reader {
	br := bufio.NewReader(r)
	// Strip UTF-8 BOM if present
	if b, _ := br.Peek(3); len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	rd := csv.NewReader(br)
	rd.FieldsPerRecord = -1    // allow variable fields per record
	rd.LazyQuotes = true       // tolerate inconsistent quotes
	rd.TrimLeadingSpace = true // trim leading spaces
	return rd
}

// readDCGMUtilCSV reads non-MIG GPU util rows (we only need static labels; values will be generated continuously)
func readDCGMUtilCSV(filename string) ([]DCGMUtilRow, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %v", err)
	}
	defer file.Close()

	reader := newCSVReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %v", err)
	}
	if len(records) < 2 {
		return nil, nil
	}
	h := headerIndexMap(records[0])
	rows := make([]DCGMUtilRow, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		r := records[i]
		get := func(k string) string {
			if idx, ok := h[k]; ok && idx < len(r) {
				return r[idx]
			}
			return ""
		}
		// usage pattern by gpu index
		deviceIdx, _ := strconv.Atoi(get("gpu"))
		pattern := "low"
		switch deviceIdx {
		case 0, 1, 2:
			pattern = "A"
		case 3, 4, 5:
			pattern = "B"
		}
		rows = append(rows, DCGMUtilRow{
			GPU:              get("gpu"),
			UUID:             get("UUID"),
			PCIBusID:         get("pci_bus_id"),
			Device:           get("device"),
			ModelName:        get("modelName"),
			Hostname:         get("Hostname"),
			DCGMFIDevMigMode: get("DCGM_FI_DEV_MIG_MODE"),
			DCGMFIDevName:    get("DCGM_FI_DEV_NAME"),
			DCGMFIDevUUID:    get("DCGM_FI_DEV_UUID"),
			DCGMFIDriverVer:  get("DCGM_FI_DRIVER_VERSION"),
			DCGMFINvmlVer:    get("DCGM_FI_NVML_VERSION"),
			Container:        get("container"),
			Namespace:        get("namespace"),
			Pod:              get("pod"),
			WtpSrc:           get("wtp_src"),
			Instance:         get("instance"),
			Node:             get("node"),
			Time:             get("time"),
			UsagePattern:     pattern,
		})
	}
	return rows, nil
}

// readGRActiveCSV reads MIG GR_ENGINE_ACTIVE rows
func readGRActiveCSV(filename string) ([]GRActiveRow, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %v", err)
	}
	defer file.Close()

	reader := newCSVReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %v", err)
	}
	if len(records) < 2 {
		return nil, nil
	}
	h := headerIndexMap(records[0])
	rows := make([]GRActiveRow, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		r := records[i]
		get := func(k string) string {
			if idx, ok := h[k]; ok && idx < len(r) {
				return r[idx]
			}
			return ""
		}
		deviceIdx, _ := strconv.Atoi(get("gpu"))
		pattern := "low"
		switch deviceIdx {
		case 0, 1, 2:
			pattern = "A"
		case 3, 4, 5:
			pattern = "B"
		}
		rows = append(rows, GRActiveRow{
			GPU:                   get("gpu"),
			UUID:                  get("UUID"),
			PCIBusID:              get("pci_bus_id"),
			Device:                get("device"),
			ModelName:             get("modelName"),
			GPU_I_PROFILE:         get("GPU_I_PROFILE"),
			GPU_I_ID:              get("GPU_I_ID"),
			Hostname:              get("Hostname"),
			DCGMFIDevMigMaxSlices: get("DCGM_FI_DEV_MIG_MAX_SLICES"),
			DCGMFIDevMigMode:      get("DCGM_FI_DEV_MIG_MODE"),
			DCGMFIDevName:         get("DCGM_FI_DEV_NAME"),
			DCGMFIDevUUID:         get("DCGM_FI_DEV_UUID"),
			DCGMFIDriverVer:       get("DCGM_FI_DRIVER_VERSION"),
			DCGMFINvmlVer:         get("DCGM_FI_NVML_VERSION"),
			Container:             get("container"),
			Namespace:             get("namespace"),
			Pod:                   get("pod"),
			WtpSrc:                get("wtp_src"),
			Instance:              get("instance"),
			Node:                  get("node"),
			Time:                  get("time"),
			UsagePattern:          pattern,
		})
	}
	return rows, nil
}

// Metric creators for raw metrics
func createUtilMetrics(utilRows []DCGMUtilRow, timestamp time.Time) []*model.OpenMx {
	metrics := make([]*model.OpenMx, 0, len(utilRows))
	weekday := timestamp.Weekday()
	for _, r := range utilRows {
		if r.DCGMFIDevMigMode != "0" { // only non-MIG are expected here
			continue
		}
		utilRatio := calculateUtilizationValue(r.UsagePattern, weekday, timestamp)
		valuePercent := utilRatio * 100.0
		mx := model.NewOpenMx("DCGM_FI_DEV_GPU_UTIL", timestamp.UnixMilli(), valuePercent)
		mx.AddLabel("gpu", r.GPU)
		mx.AddLabel("UUID", r.UUID)
		mx.AddLabel("pci_bus_id", r.PCIBusID)
		mx.AddLabel("device", r.Device)
		mx.AddLabel("modelName", r.ModelName)
		mx.AddLabel("Hostname", r.Hostname)
		mx.AddLabel("DCGM_FI_DEV_MIG_MODE", r.DCGMFIDevMigMode)
		mx.AddLabel("DCGM_FI_DEV_NAME", r.DCGMFIDevName)
		mx.AddLabel("DCGM_FI_DEV_UUID", r.DCGMFIDevUUID)
		mx.AddLabel("DCGM_FI_DRIVER_VERSION", r.DCGMFIDriverVer)
		mx.AddLabel("DCGM_FI_NVML_VERSION", r.DCGMFINvmlVer)
		mx.AddLabel("container", r.Container)
		mx.AddLabel("namespace", r.Namespace)
		mx.AddLabel("pod", r.Pod)
		mx.AddLabel("wtp_src", r.WtpSrc)
		mx.AddLabel("instance", r.Instance)
		mx.AddLabel("node", r.Node)
		metrics = append(metrics, mx)
	}
	return metrics
}

func createGRMetrics(grRows []GRActiveRow, timestamp time.Time) []*model.OpenMx {
	metrics := make([]*model.OpenMx, 0, len(grRows))
	weekday := timestamp.Weekday()
	for _, r := range grRows {
		if r.DCGMFIDevMigMode != "1" { // only MIG here
			continue
		}
		value := calculateUtilizationValue(r.UsagePattern, weekday, timestamp) // 0..1
		mx := model.NewOpenMx("DCGM_FI_PROF_GR_ENGINE_ACTIVE", timestamp.UnixMilli(), value)
		mx.AddLabel("gpu", r.GPU)
		mx.AddLabel("UUID", r.UUID)
		mx.AddLabel("pci_bus_id", r.PCIBusID)
		mx.AddLabel("device", r.Device)
		mx.AddLabel("modelName", r.ModelName)
		mx.AddLabel("GPU_I_PROFILE", r.GPU_I_PROFILE)
		mx.AddLabel("GPU_I_ID", r.GPU_I_ID)
		mx.AddLabel("Hostname", r.Hostname)
		mx.AddLabel("DCGM_FI_DEV_MIG_MAX_SLICES", r.DCGMFIDevMigMaxSlices)
		mx.AddLabel("DCGM_FI_DEV_MIG_MODE", r.DCGMFIDevMigMode)
		mx.AddLabel("DCGM_FI_DEV_NAME", r.DCGMFIDevName)
		mx.AddLabel("DCGM_FI_DEV_UUID", r.DCGMFIDevUUID)
		mx.AddLabel("DCGM_FI_DRIVER_VERSION", r.DCGMFIDriverVer)
		mx.AddLabel("DCGM_FI_NVML_VERSION", r.DCGMFINvmlVer)
		mx.AddLabel("container", r.Container)
		mx.AddLabel("namespace", r.Namespace)
		mx.AddLabel("pod", r.Pod)
		mx.AddLabel("wtp_src", r.WtpSrc)
		mx.AddLabel("instance", r.Instance)
		mx.AddLabel("node", r.Node)
		metrics = append(metrics, mx)
	}
	return metrics
}

// logDCGMMessage logs a message to both the file logger and stdout
func logDCGMMessage(logger *logfile.FileLogger, tag string, message string) {
	logger.Println(tag, message)
	fmt.Printf("[%s] %s\n", tag, message)
}

// processDCGM creates and sends raw DCGM metrics (GPU_UTIL and GR_ENGINE_ACTIVE) using the secure.Send method
func processDCGM(logger *logfile.FileLogger, utilRows []DCGMUtilRow, grRows []GRActiveRow, timestamp time.Time) error {
	// Create metrics from DCGM data (two kinds)
	utilMetrics := createUtilMetrics(utilRows, timestamp)
	grMetrics := createGRMetrics(grRows, timestamp)
	metrics := append(utilMetrics, grMetrics...)
	logDCGMMessage(logger, "DCGMExporter", fmt.Sprintf("Created %d DCGM metrics (GPU_UTIL=%d, GR_ENGINE_ACTIVE=%d) for %s", len(metrics), len(utilMetrics), len(grMetrics), timestamp.Format("2006/01/02 15:04:05")))

	// Create help information for both metrics
	helpItems := make([]*model.OpenMxHelp, 0, 2)
	mxh1 := model.NewOpenMxHelp("DCGM_FI_DEV_GPU_UTIL")
	mxh1.Put("help", "GPU utilization percent (0..100)")
	mxh1.Put("type", "gauge")
	helpItems = append(helpItems, mxh1)
	mxh2 := model.NewOpenMxHelp("DCGM_FI_PROF_GR_ENGINE_ACTIVE")
	mxh2.Put("help", "Graphics engine active ratio per MIG instance (0..1)")
	mxh2.Put("type", "gauge")
	helpItems = append(helpItems, mxh2)

	// Send help information
	if len(helpItems) > 0 {
		helpPack := model.NewOpenMxHelpPack()
		securityMaster := secure.GetSecurityMaster()
		if securityMaster == nil {
			return fmt.Errorf("no security master available")
		}

		// Set PCODE and OID
		helpPack.SetPCODE(securityMaster.PCODE)
		helpPack.SetOID(securityMaster.OID)
		helpPack.SetTime(timestamp.UnixMilli())
		helpPack.SetRecords(helpItems)

		logDCGMMessage(logger, "DCGMExporter", fmt.Sprintf("Sending %d help records", len(helpItems)))
		secure.Send(secure.NET_SECURE_HIDE, helpPack, true)
		time.Sleep(100 * time.Millisecond)
	}

	// Send metrics
	metricsPack := model.NewOpenMxPack()
	metricsPack.SetTime(timestamp.UnixMilli())
	metricsPack.SetRecords(metrics)

	// Get the security master
	securityMaster := secure.GetSecurityMaster()
	if securityMaster == nil {
		return fmt.Errorf("no security master available")
	}

	// Set PCODE and OID
	metricsPack.SetPCODE(securityMaster.PCODE)
	metricsPack.SetOID(securityMaster.OID)

	logDCGMMessage(logger, "DCGMExporter", fmt.Sprintf("Sending %d metrics", len(metrics)))
	secure.Send(secure.NET_SECURE_HIDE, metricsPack, true)

	return nil
}

func main() {
	// Check if environment variables are set
	license := os.Getenv("WHATAP_LICENSE")
	host := os.Getenv("WHATAP_HOST")
	port := os.Getenv("WHATAP_PORT")
	license = "x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502"
	host = "15.165.146.117"
	port = "6600"

	// Create a logger
	logger := logfile.NewFileLogger()
	logDCGMMessage(logger, "DCGMExporter", "Starting DCGM GPU Utilization Metrics Exporter")

	// Initialize secure communication
	servers := []string{fmt.Sprintf("%s:%s", host, port)}
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("dcgm-exporter"))

	// Seed random number generator
	rand.Seed(time.Now().UnixNano())

	// Read DCGM CSV data (util and GR_ENGINE_ACTIVE)
	utilRows, err := readDCGMUtilCSV("../mock/DCGM_FI_DEV_GPU_UTIL.csv")
	if err != nil {
		log.Fatalf("Failed to read GPU_UTIL CSV: %v", err)
	}
	grRows, err := readGRActiveCSV("../mock/DCGM_FI_PROF_GR_ENGINE_ACTIVE.csv")
	if err != nil {
		log.Fatalf("Failed to read GR_ENGINE_ACTIVE CSV: %v", err)
	}

	logDCGMMessage(logger, "DCGMExporter", fmt.Sprintf("Loaded %d GPU_UTIL rows and %d GR_ENGINE_ACTIVE rows from CSV", len(utilRows), len(grRows)))

	// Load Korean timezone (KST)
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		log.Fatalf("Failed to load Korean timezone: %v", err)
	}

	// Start real-time streaming from current time (KST)
	logDCGMMessage(logger, "DCGMExporter", "Starting real-time sending of raw DCGM metrics (GPU_UTIL and GR_ENGINE_ACTIVE)")
	logDCGMMessage(logger, "DCGMExporter", "Sending metrics every 30 seconds using secure.Send")
	logDCGMMessage(logger, "DCGMExporter", fmt.Sprintf("Usage patterns: A devices (Mon-Tue high), B devices (Wed-Thu-Fri high), Low devices (consistently low)"))

	intervalCount := 0
	for {
		intervalCount++
		now := time.Now().In(kst)

		// Process and send metrics using secure.Send
		err := processDCGM(logger, utilRows, grRows, now)
		if err != nil {
			logDCGMMessage(logger, "DCGMExporter", fmt.Sprintf("Failed to send metrics for %s: %v", now.Format("2006/01/02 15:04:05"), err))
		} else {
			if intervalCount%10 == 1 { // periodic heartbeat
				logDCGMMessage(logger, "DCGMExporter", fmt.Sprintf("✓ Sent metrics at %s (%s)", now.Format("2006/01/02 15:04:05"), now.Weekday().String()))
			}
		}

		// Sleep to next 30-second tick
		time.Sleep(30 * time.Second)
	}
}
