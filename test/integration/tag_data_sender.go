package main

import (
	"fmt"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
	"open-agent/pkg/common"
	"os"
	"time"
)

// logMessage logs a message to both the file logger and stdout
func logMessage(logger *logfile.FileLogger, tag string, message string) {
	logger.Println(tag, message)
	fmt.Printf("[%s] %s\n", tag, message)
}

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
	logMessage(logger, "TagDataSender", "Starting tag data sender")

	// Initialize secure communication
	servers := []string{fmt.Sprintf("%s:%s", host, port)}

	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("test"))

	// Get the security master from the secure package
	securityMaster := secure.GetSecurityMaster()
	if securityMaster == nil {
		logMessage(logger, "TagDataSender", "No security master available")
		return
	}

	// Create a TagData object
	td := common.NewTagData()
	// Add some tags
	td.AddTag("test", "jykim")
	td.AddTag("agent", "open")
	td.AddTag("lang", "go")

	// Log the tags
	logMessage(logger, "TagDataSender", fmt.Sprintf("PCODE=%d", securityMaster.PCODE))
	logMessage(logger, "TagDataSender", "Sending tag data to WHATAP server")

	// Send the tag data
	td.SendTagData(securityMaster.PCODE)

	// Wait for the data to be sent
	time.Sleep(1 * time.Second)

	logMessage(logger, "TagDataSender", "Tag data sent successfully")
}
