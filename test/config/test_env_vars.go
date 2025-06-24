package main

import (
	"fmt"
	whatap_config "open-agent/pkg/config"
	"os"
)

func main() {
	// Set an environment variable
	os.Setenv("DEBUG", "true")
	os.Setenv("WHATAP_SERVER_PORT", "9090")
	os.Setenv("WHATAP_LOG_LEVEL", "debug")

	// Get values using the Get method
	fmt.Println("Values from Get method:")
	fmt.Printf("debug: %s\n", whatap_config.Get("debug"))
	fmt.Printf("WHATAP_SERVER_PORT: %s\n", whatap_config.Get("WHATAP_SERVER_PORT"))
	fmt.Printf("log_level: %s\n", whatap_config.Get("log_level"))
	fmt.Println("-----------------------------------")

	// Get values using the GetConfig method
	fmt.Println("Values from GetConfig method:")
	config := whatap_config.GetConfig()
	fmt.Printf("Debug: %v\n", config.Debug)
	fmt.Printf("ServerPort: %d\n", config.ServerPort)
	fmt.Printf("LogLevel: %s\n", config.LogLevel)
	fmt.Println("-----------------------------------")

	// Clean up
	os.Unsetenv("WHATAP_DEBUG")
	os.Unsetenv("WHATAP_SERVER_PORT")
	os.Unsetenv("WHATAP_LOG_LEVEL")
	whatap_config.Cleanup()
}
