//it has to be created as for this current version of the code, the server node is having power usage data based on different metrics and devices.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/cors"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/StackExchange/wmi"
)

// Node structure for server node details
type Node struct {
	ID        string  `json:"id"`
	IPAddress string  `json:"ip_address"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Port      string  `json:"port"`
	Status    string  `json:"status"`
}

var serverNode Node

// Function to get the public IP address of the machine
func getPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ip, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(ip), nil
}

// Function to get geolocation using an external API
func getGeoLocation(ip string) (float64, float64, error) {
	geoAPI := fmt.Sprintf("https://ip-api.com/json/%s", ip)
	resp, err := http.Get(geoAPI)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var geoData struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	}
	err = json.NewDecoder(resp.Body).Decode(&geoData)
	if err != nil {
		return 0, 0, err
	}

	return geoData.Lat, geoData.Lon, nil
}

// Function to get local IP address of the machine
func getLocalIPAddress() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}

	return "", fmt.Errorf("no IP address found")
}

// Function to capture system resource usage data
func captureSystemUsage() (map[string]interface{}, error) {
	memoryStats, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	cpuUsage, err := cpu.Percent(time.Second, false)
	if err != nil {
		return nil, err
	}

	loadStats, err := load.Avg()
	if err != nil {
		return nil, err
	}

	hostInfo, err := host.Info()
	if err != nil {
		return nil, err
	}

	powerUsage, err := getPowerUsage() // Capture actual power usage
	if err != nil {
		log.Println("Error fetching power usage:", err)
	}

	usageData := map[string]interface{}{
		"Memory Total":      memoryStats.Total / (1024 * 1024),
		"Memory Used":       memoryStats.Used / (1024 * 1024),
		"Memory Used %":     memoryStats.UsedPercent,
		"CPU Usage %":       cpuUsage[0],
		"Load Average (1m)": loadStats.Load1,
		"Uptime":            hostInfo.Uptime,
		"Power Usage (W)":   powerUsage, // Actual power usage
	}

	return usageData, nil
}

// Function to fetch power usage data on Windows using WMI
func getPowerUsage() (float64, error) {
	var powerStatus []struct {
		PowerState int32
	}

	// Query WMI for power status information
	err := wmi.Query("SELECT * FROM Win32_Battery", &powerStatus)
	if err != nil {
		return 0, err
	}

	if len(powerStatus) == 0 {
		return 0, fmt.Errorf("No power data found")
	}

	// Example of returning battery status as a power estimate (in watts)
	// In practice, you'd need to adjust this based on actual power metrics.
	return float64(powerStatus[0].PowerState), nil
}

// Function to save system and client data to a CSV file (active log)
func saveActiveLog(clientIP string, clientLatitude, clientLongitude float64, nodeLatitude, nodeLongitude float64, latency float64, timestamp string, clientData string, systemUsage map[string]interface{}) {
	fileName := "active_log_ServerNode.csv"

	// Check if the CSV file exists, and if not, create it and add headers
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		file, err := os.Create(fileName)
		if err != nil {
			log.Printf("Error creating active log CSV file: %v\n", err)
			return
		}
		defer file.Close()

		// Write header row
		headers := "ClientIP,ClientLatitude,ClientLongitude,NodeLatitude,NodeLongitude,Latency,Timestamp,ClientData,MemoryTotal,MemoryUsed,MemoryUsedPercent,CPUUsage,LoadAvg,Uptime,PowerUsage\n"
		if _, err := file.WriteString(headers); err != nil {
			log.Printf("Error writing headers to active log CSV file: %v\n", err)
		}
	}

	// Open CSV file in append mode
	file, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening active log CSV file: %v\n", err)
		return
	}
	defer file.Close()

	// Format: ClientIP, ClientLatitude, ClientLongitude, NodeLatitude, NodeLongitude, Latency, Timestamp, ClientData, System Usage
	logEntry := fmt.Sprintf(
		"%s,%.6f,%.6f,%.6f,%.6f,%.3f,%s,%s,%.2f,%.2f,%.2f,%.2f,%.2f,%d,%.2f\n",
		clientIP,
		clientLatitude,
		clientLongitude,
		nodeLatitude,
		nodeLongitude,
		latency,
		timestamp,
		clientData,
		systemUsage["Memory Total"],
		systemUsage["Memory Used"],
		systemUsage["Memory Used %"],
		systemUsage["CPU Usage %"],
		systemUsage["Load Average (1m)"],
		systemUsage["Uptime"],
		systemUsage["Power Usage (W)"],
	)
	if _, err := file.WriteString(logEntry); err != nil {
		log.Printf("Error writing to active log CSV file: %v\n", err)
	}
}

// Function to save passive logs (background server operations)
func savePassiveLog(activity string) {
	fileName := "passive_log_ServerNode.csv"

	// Check if the CSV file exists, and if not, create it and add headers
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		file, err := os.Create(fileName)
		if err != nil {
			log.Printf("Error creating passive log CSV file: %v\n", err)
			return
		}
		defer file.Close()

		// Write header row
		headers := "Timestamp,Activity\n"
		if _, err := file.WriteString(headers); err != nil {
			log.Printf("Error writing headers to passive log CSV file: %v\n", err)
		}
	}

	// Open CSV file in append mode
	file, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening passive log CSV file: %v\n", err)
		return
	}
	defer file.Close()

	// Format: Timestamp, Activity
	logEntry := fmt.Sprintf(
		"%s,%s\n",
		time.Now().Format("2006-01-02T15:04:05-07:00"),
		activity,
	)
	if _, err := file.WriteString(logEntry); err != nil {
		log.Printf("Error writing to passive log CSV file: %v\n", err)
	}
}

// Function to self-register the server node with the main server
func selfRegister(mainServerURL string, node Node) {
	data, err := json.Marshal(node)
	if err != nil {
		log.Println("Error marshalling node data:", err)
		return
	}

	log.Println("Attempting to register with the main server...")
	resp, err := http.Post(mainServerURL+"/register-node", "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Println("Error registering node with the main server:", err)
		return
	}
	defer resp.Body.Close()

	responseBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response body:", err)
		return
	}

	log.Printf("Main server response: %s\n", string(responseBody))
	if resp.StatusCode == http.StatusOK {
		log.Println("Node successfully registered with the main server.")
		savePassiveLog("Node registered with main server")
	} else {
		log.Printf("Failed to register node. Status code: %d\n", resp.StatusCode)
		savePassiveLog("Node registration failed")
	}
}

// Handler for health check endpoint
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	savePassiveLog("Health check received")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"active"}`))
}

// Handler for incoming requests (e.g., for receiving data/files)
func handleRequest(w http.ResponseWriter, r *http.Request) {
	clientIP := r.RemoteAddr
	requestBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Fetch the client's geolocation
	clientLatitude, clientLongitude, err := getGeoLocation(clientIP)
	if err != nil {
		log.Printf("Error fetching client geolocation: %v\n", err)
	}

	// Capture system usage data
	usageData, err := captureSystemUsage()
	if err != nil {
		log.Printf("Error capturing system usage data: %v\n", err)
	}

	// Log the system usage data (you can choose to print it or save it)
	log.Printf("System Usage Data: %+v\n", usageData)

	// Get the current timestamp
	timestamp := time.Now().Format("2006-01-02T15:04:05-07:00")

	// Prepare client data
	clientData := string(requestBody)

	// Save data to active log (interaction)
	latency := 0.0 // Example: Simulate a constant latency, modify accordingly
	saveActiveLog(clientIP, clientLatitude, clientLongitude, serverNode.Latitude, serverNode.Longitude, latency, timestamp, clientData, usageData)

	// Prepare the JSON response
	response := map[string]string{
		"status":  "ok",
		"message": "Request received and processed",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	// Save passive log for background operation
	savePassiveLog("Request received and processed")
}

func main() {
	log.Println("Starting server node...")

	// Fetch public IP address
	publicIP, err := getPublicIP()
	if err != nil {
		log.Println("Error fetching public IP address:", err)
		return
	}
	log.Println("Public IP Address:", publicIP)

	// Fetch geolocation data
	latitude, longitude, err := getGeoLocation(publicIP)
	if err != nil {
		log.Println("Error fetching geolocation:", err)
		return
	}
	log.Printf("Geolocation fetched: Latitude: %.6f, Longitude: %.6f\n", latitude, longitude)

	// Fetch local IP address
	localIP, err := getLocalIPAddress()
	if err != nil {
		log.Println("Error fetching local IP address:", err)
		return
	}
	log.Println("Local IP Address:", localIP)

	// Generate unique node ID
	nodeID := uuid.New().String()

	// Node information
	port := "8081"
	serverNode = Node{
		ID:        nodeID,
		IPAddress: localIP,
		Latitude:  latitude,
		Longitude: longitude,
		Port:      port,
		Status:    "active",
	}

	// Main server URL
	mainServerURL := "http://localhost:8080" // Replace with actual main server URL

	// Self-register with the main server
	selfRegister(mainServerURL, serverNode)

	// Set up HTTP server
	http.HandleFunc("/receive", handleRequest)
	http.HandleFunc("/health", healthCheckHandler)

	// Enable CORS for all domains
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	})

	server := &http.Server{
		Addr:    ":" + port,
		Handler: c.Handler(http.DefaultServeMux),
	}

	// Graceful shutdown
	go func() {
		log.Printf("Server listening on port %s...\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %v", err)
		}
	}()

	// Wait for interrupt signal to shut down gracefully
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")
	server.Close()
}