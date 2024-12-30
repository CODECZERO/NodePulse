// Full working code with the requested changes
package main

import (
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

	"github.com/StackExchange/wmi"
	"github.com/google/uuid"
	"github.com/rs/cors"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
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

	powerUsage, err := getPowerUsage()
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
		"Power Usage (W)":   powerUsage,
	}

	return usageData, nil
}

// Function to fetch power usage data on Windows using WMI
func getPowerUsage() (float64, error) {
	var powerStatus []struct {
		PowerState int32
	}

	err := wmi.Query("SELECT * FROM Win32_Battery", &powerStatus)
	if err != nil {
		return 0, err
	}

	if len(powerStatus) == 0 {
		return 0, fmt.Errorf("No power data found")
	}

	return float64(powerStatus[0].PowerState), nil
}

// Function to measure network latency
func measureLatency(clientIP string) (float64, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", clientIP, time.Second*5)
	if err != nil {
		return 0, err
	}
	conn.Close()
	latency := time.Since(start).Seconds() * 1000
	return latency, nil
}

// Function to save logs in a specified folder
func saveLogFile(folder string, fileName string, content string) {
	if _, err := os.Stat(folder); os.IsNotExist(err) {
		err := os.MkdirAll(folder, 0755)
		if err != nil {
			log.Printf("Error creating folder %s: %v\n", folder, err)
			return
		}
	}

	filePath := folder + "/" + fileName
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		log.Printf("Error writing to file %s: %v\n", filePath, err)
	}
}

// Function to save system and client data to a CSV file
func saveActiveLog(folder string, clientIP string, clientLatitude, clientLongitude, nodeLatitude, nodeLongitude, latency float64, timestamp string, clientData string, systemUsage map[string]interface{}) {
	fileName := "active_log_ServerNode.csv"
	headers := "ClientIP,ClientLatitude,ClientLongitude,NodeLatitude,NodeLongitude,Latency,Timestamp,ClientData,MemoryTotal,MemoryUsed,MemoryUsedPercent,CPUUsage,LoadAvg,Uptime,PowerUsage\n"

	if _, err := os.Stat(folder + "/" + fileName); os.IsNotExist(err) {
		saveLogFile(folder, fileName, headers)
	}

	logEntry := fmt.Sprintf(
		"%s,%.6f,%.6f,%.6f,%.6f,%.3f,%s,%s,%.2f,%.2f,%.2f,%.2f,%.2f,%d,%.2f\n",
		clientIP, clientLatitude, clientLongitude, nodeLatitude, nodeLongitude, latency, timestamp, clientData,
		systemUsage["Memory Total"], systemUsage["Memory Used"], systemUsage["Memory Used %"],
		systemUsage["CPU Usage %"], systemUsage["Load Average (1m)"],
		systemUsage["Uptime"], systemUsage["Power Usage (W)"],
	)
	saveLogFile(folder, fileName, logEntry)
}

// Function to save passive logs
func savePassiveLog(folder string, activity string) {
	fileName := "passive_log_ServerNode.csv"
	headers := "Timestamp,Activity\n"
	if _, err := os.Stat(folder + "/" + fileName); os.IsNotExist(err) {
		saveLogFile(folder, fileName, headers)
	}

	logEntry := fmt.Sprintf(
		"%s,%s\n",
		time.Now().Format("2006-01-02T15:04:05-07:00"), activity,
	)
	saveLogFile(folder, fileName, logEntry)
}

// Handler for incoming requests
func handleRequest(w http.ResponseWriter, r *http.Request) {
	clientIP := r.RemoteAddr
	requestBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	clientLatitude, clientLongitude, err := getGeoLocation(clientIP)
	if err != nil {
		log.Printf("Error fetching client geolocation: %v\n", err)
	}

	systemUsage, err := captureSystemUsage()
	if err != nil {
		log.Printf("Error capturing system usage data: %v\n", err)
	}

	latency, err := measureLatency(clientIP)
	if err != nil {
		log.Printf("Error measuring latency: %v\n", err)
	}

	timestamp := time.Now().Format("2006-01-02T15:04:05-07:00")
	clientData := string(requestBody)

	folder := "ServerNodeData"
	saveActiveLog(folder, clientIP, clientLatitude, clientLongitude, serverNode.Latitude, serverNode.Longitude, latency, timestamp, clientData, systemUsage)

	response := map[string]string{
		"status":  "ok",
		"message": "Request received and processed",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	savePassiveLog(folder, "Request received and processed")
}

func main() {
	log.Println("Starting server node...")

	// Create the directory for server node data
	dataDir := "ServerNodeData"
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		if err := os.Mkdir(dataDir, 0755); err != nil {
			log.Fatalf("Error creating data directory: %v\n", err)
		}
	}
	log.Printf("Data directory initialized at: %s\n", dataDir)

	// Fetch public IP address
	publicIP, err := getPublicIP()
	if err != nil {
		log.Fatalf("Error fetching public IP address: %v\n", err)
	}
	log.Println("Public IP Address:", publicIP)

	// Fetch geolocation data
	latitude, longitude, err := getGeoLocation(publicIP)
	if err != nil {
		log.Fatalf("Error fetching geolocation: %v\n", err)
	}
	log.Printf("Geolocation fetched: Latitude: %.6f, Longitude: %.6f\n", latitude, longitude)

	// Fetch local IP address
	localIP, err := getLocalIPAddress()
	if err != nil {
		log.Fatalf("Error fetching local IP address: %v\n", err)
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
	http.HandleFunc("/receive", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, dataDir)
	})
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
			log.Fatalf("Error starting server: %v\n", err)
		}
	}()

	// Wait for interrupt signal to shut down gracefully
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")
	if err := server.Close(); err != nil {
		log.Fatalf("Error shutting down server: %v\n", err)
	}
}
