package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
	"open-agent/pkg/model"
)

// GPUProcessData represents a single row from the CSV
type GPUProcessData struct {
	Device  string
	PID     string
	Command string
	Type    string
	UUID    string
	Time    string
}

// readCSVData reads the CSV file and returns the data
func readCSVData(filename string) ([]GPUProcessData, error) {
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

	var data []GPUProcessData
	// Skip header row
	for i := 1; i < len(records); i++ {
		if len(records[i]) >= 6 {
			data = append(data, GPUProcessData{
				Device:  records[i][0],
				PID:     records[i][1],
				Command: records[i][2],
				Type:    records[i][3],
				UUID:    records[i][4],
				Time:    records[i][5],
			})
		}
	}

	return data, nil
}

// createMetricsFromCSV creates OpenMx metrics from CSV data
func createMetricsFromCSV(data []GPUProcessData, timestamp time.Time) []*model.OpenMx {
	metrics := make([]*model.OpenMx, 0, len(data))

	for _, process := range data {
		// Generate a random utilization value between 0-100
		utilization := rand.Float64() * 100

		// Create OpenMx metric
		mx := model.NewOpenMx("DCGM_GPU_PROCESS_UTIL", timestamp.UnixMilli(), utilization)

		// Add labels
		mx.AddLabel("instance", "http://10.21.150.223:9400/metrics/process")
		mx.AddLabel("node", "ip-10-21-150-58.ec2.internal")
		mx.AddLabel("device", process.Device)
		mx.AddLabel("pid", process.PID)
		mx.AddLabel("command", process.Command)
		mx.AddLabel("type", process.Type)
		mx.AddLabel("UUID", process.UUID)

		metrics = append(metrics, mx)
	}

	return metrics
}

// logMessage logs a message to both the file logger and stdout
func logMessage(logger *logfile.FileLogger, tag string, message string) {
	logger.Println(tag, message)
	fmt.Printf("[%s] %s\n", tag, message)
}

// process creates and sends metrics using the secure.Send method
func process1(logger *logfile.FileLogger, data []GPUProcessData, timestamp time.Time) error {
	// Create metrics from CSV data
	metrics := createMetricsFromCSV(data, timestamp)
	logMessage(logger, "GPUExporter", fmt.Sprintf("Created %d DCGM_GPU_PROCESS_UTIL metrics for %s", len(metrics), timestamp.Format("2006/01/02 15:04:05")))

	// Create help information
	helpItems := make([]*model.OpenMxHelp, 0)
	mxh := model.NewOpenMxHelp("DCGM_GPU_PROCESS_UTIL")
	mxh.Put("help", "GPU process utilization percentage")
	mxh.Put("type", "gauge")
	helpItems = append(helpItems, mxh)

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

		logMessage(logger, "GPUExporter", fmt.Sprintf("Sending %d help records", len(helpItems)))
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

	logMessage(logger, "GPUExporter", fmt.Sprintf("Sending %d metrics", len(metrics)))
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
	//if license == "" || host == "" || port == "" {
	//	fmt.Println("Please set the following environment variables:")
	//	fmt.Println("WHATAP_LICENSE - The license key for the WHATAP server")
	//	fmt.Println("WHATAP_HOST - The hostname or IP address of the WHATAP server")
	//	fmt.Println("WHATAP_PORT - The port number of the WHATAP server")
	//	os.Exit(1)
	//}

	// Create a logger
	logger := logfile.NewFileLogger()
	logMessage(logger, "GPUExporter", "Starting GPU Process Metrics Exporter")

	// Initialize secure communication
	servers := []string{fmt.Sprintf("%s:%s", host, port)}
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("gpu-exporter"))

	// Seed random number generator
	rand.Seed(time.Now().UnixNano())

	// Read CSV data
	csvFile := "../mock/nvidia-gpu-t4-k8s.csv"
	data, err := readCSVData(csvFile)
	if err != nil {
		log.Fatalf("Failed to read CSV data: %v", err)
	}

	logMessage(logger, "GPUExporter", fmt.Sprintf("Loaded %d GPU process records from CSV", len(data)))

	// Set up time range: 2025/07/22 20:10:00 to 20:40:00 (30 minutes) - Korean timezone (+9 hours)
	// Load Korean timezone (KST)
	kst, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		log.Fatalf("Failed to load Korean timezone: %v", err)
	}

	startTime, err := time.ParseInLocation("2006/01/02 15:04:05", "2025/07/22 20:10:00", kst)
	if err != nil {
		log.Fatalf("Failed to parse start time: %v", err)
	}

	endTime, err := time.ParseInLocation("2006/01/02 15:04:05", "2025/07/22 20:40:00", kst)
	if err != nil {
		log.Fatalf("Failed to parse end time: %v", err)
	}

	logMessage(logger, "GPUExporter", fmt.Sprintf("Starting to send DCGM_GPU_PROCESS_UTIL metrics from %s to %s",
		startTime.Format("2006/01/02 15:04:05"),
		endTime.Format("2006/01/02 15:04:05")))
	logMessage(logger, "GPUExporter", "Sending metrics every 30 seconds using secure.Send")

	// Generate and send metrics every 30 seconds
	currentTime := startTime
	for currentTime.Before(endTime) || currentTime.Equal(endTime) {
		// Process and send metrics using secure.Send
		err := process1(logger, data, currentTime)
		if err != nil {
			logMessage(logger, "GPUExporter", fmt.Sprintf("Failed to send metrics for %s: %v", currentTime.Format("2006/01/02 15:04:05"), err))
		} else {
			logMessage(logger, "GPUExporter", fmt.Sprintf("✓ Successfully sent metrics for %s", currentTime.Format("2006/01/02 15:04:05")))
		}

		// Move to next 30-second interval
		currentTime = currentTime.Add(30 * time.Second)

		// Add a small delay to avoid overwhelming the endpoint
		time.Sleep(100 * time.Millisecond)
	}

	logMessage(logger, "GPUExporter", "=== Finished sending all metrics ===")
}
