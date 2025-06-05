package main

import (
	"fmt"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
	"open-agent/pkg"
	"open-agent/pkg/model"
	"open-agent/pkg/sender"
	"os"
	"time"
)

func main() {
	// Check if environment variables are set
	license := os.Getenv("WHATAP_LICENSE")
	host := os.Getenv("WHATAP_HOST")
	port := os.Getenv("WHATAP_PORT")

	if license == "" || host == "" || port == "" {
		fmt.Println("Please set the following environment variables:")
		fmt.Println("WHATAP_LICENSE - The license key for the WHATAP server")
		fmt.Println("WHATAP_HOST - The hostname or IP address of the WHATAP server")
		fmt.Println("WHATAP_PORT - The port number of the WHATAP server")
		os.Exit(1)
	}

	// Create a logger
	logger := logfile.NewFileLogger()
	logger.Println("SampleDataSender", "Starting sample data sender")

	// Initialize secure communication
	servers := []string{fmt.Sprintf("%s:%s", host, port)}
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers))

	// Create a channel for processed metrics
	processedQueue := make(chan *model.ConversionResult, 1000)

	// Create a sender
	senderInstance := sender.NewSender(processedQueue, logger)
	senderInstance.Start()

	// Create a sample data sender
	sampleDataSender := pkg.NewSampleDataSender(processedQueue)

	// Send the sample data
	logger.Println("SampleDataSender", "Sending sample data to WHATAP server")
	sampleDataSender.SendSampleData()

	// Wait for the data to be sent
	logger.Println("SampleDataSender", "Waiting for data to be sent")
	time.Sleep(5 * time.Second)

	// Stop the sender
	logger.Println("SampleDataSender", "Stopping sender")
	senderInstance.Stop()

	logger.Println("SampleDataSender", "Sample data sent successfully")
}
