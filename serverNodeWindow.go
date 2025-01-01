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
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rs/cors"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
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

const logFolder = "serverNodeData"

// Ensure log folder exists
func ensureLogFolder() error {
	if _, err := os.Stat(logFolder); os.IsNotExist(err) {
		return os.Mkdir(logFolder, 0755)
	}
	return nil
}

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
// Function to get power consumption details
// Function to get power consumption details
func getPowerConsumption() (map[string]interface{}, error) {
	// WMI data - Battery Status Information
	var batteryInfo []struct {
		EstimatedChargeRemaining uint8
		PowerOnline              bool
		ACLineStatus             uint8
	}
	err := wmi.Query("SELECT * FROM Win32_Battery", &batteryInfo)
	if err != nil {
		return nil, err
	}

	// WMI data - Battery Status - AC Power Information
	var acPowerInfo []struct {
		PowerState uint16
	}
	err = wmi.Query("SELECT * FROM Win32_DesktopMonitor", &acPowerInfo)
	if err != nil {
		return nil, err
	}

	// Prepare the power data map
	powerData := map[string]interface{}{
		"Battery Percentage":     batteryInfo[0].EstimatedChargeRemaining,
		"Is Charging":            batteryInfo[0].PowerOnline,
		"Time Remaining (hrs)":   "Not available", // WMI doesn't provide exact remaining time, so you'll need a different approach or estimation.
		"AC Power":               batteryInfo[0].ACLineStatus == 1, // 1 means plugged in, 0 means not plugged in
	}

	return powerData, nil
}


// Function to capture system resource usage data along with power consumption and WMI data
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

	// Get power consumption details
	powerData, err := getPowerConsumption()
	if err != nil {
		return nil, err
	}

	// WMI data - Operating System Information
	var osInfo []struct {
		Name     string
		Version  string
		Architecture string
	}
	err = wmi.Query("SELECT * FROM Win32_OperatingSystem", &osInfo)
	if err != nil {
		return nil, err
	}

	// WMI data - CPU Information
	var cpuInfo []struct {
		DeviceID  string
		Name      string
		LoadPercentage uint8
	}
	err = wmi.Query("SELECT * FROM Win32_Processor", &cpuInfo)
	if err != nil {
		return nil, err
	}

	// Prepare the usage data
	usageData := map[string]interface{}{
		"Memory Total":      memoryStats.Total / (1024 * 1024),
		"Memory Used":       memoryStats.Used / (1024 * 1024),
		"Memory Used %":     memoryStats.UsedPercent,
		"CPU Usage %":       cpuUsage[0],
		"Load Average (1m)": loadStats.Load1,
		"Uptime":            hostInfo.Uptime,
		"Battery Percentage": powerData["Battery Percentage"],
		"Is Charging":        powerData["Is Charging"],
		"Time Remaining (hrs)": powerData["Time Remaining (hrs)"],
		"AC Power":           powerData["AC Power"],
		"OS Name":            osInfo[0].Name,
		"OS Version":         osInfo[0].Version,
		"OS Architecture":    osInfo[0].Architecture,
		"CPU Model":          cpuInfo[0].Name,
		"CPU Load %":         cpuInfo[0].LoadPercentage,
	}

	return usageData, nil
}

// Function to save system and client data to a CSV file (active log)
func saveActiveLog(clientIP string, clientLatitude, clientLongitude float64, nodeLatitude, nodeLongitude float64, latency float64, timestamp string, clientData string, systemUsage map[string]interface{}) {
	err := ensureLogFolder()
	if err != nil {
		log.Printf("Error ensuring log folder: %v\n", err)
		return
	}

	fileName := filepath.Join(logFolder, "active_log_ServerNode.csv")

	// Check if the CSV file exists, and if not, create it and add headers
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		file, err := os.Create(fileName)
		if err != nil {
			log.Printf("Error creating active log CSV file: %v\n", err)
			return
		}
		defer file.Close()

		// Write header row
		headers := "ClientIP,ClientLatitude,ClientLongitude,NodeLatitude,NodeLongitude,Latency,Timestamp,ClientData,MemoryTotal,MemoryUsed,MemoryUsedPercent,CPUUsage,LoadAvg,Uptime\n"
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

	logEntry := fmt.Sprintf(
		"%s,%.6f,%.6f,%.6f,%.6f,%.3f,%s,%s,%.2f,%.2f,%.2f,%.2f,%.2f,%d,%.2f,%v,%d,%v,%s,%s,%s,%s,%d\n",
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
		systemUsage["Battery Percentage"],
		systemUsage["Is Charging"],
		systemUsage["Time Remaining (hrs)"],
		systemUsage["AC Power"],
		systemUsage["OS Name"],
		systemUsage["OS Version"],
		systemUsage["OS Architecture"],
		systemUsage["CPU Model"],
		systemUsage["CPU Load %"],
	)
	
	if _, err := file.WriteString(logEntry); err != nil {
		log.Printf("Error writing to active log CSV file: %v\n", err)
	}
}

// Function to save passive logs (background server operations)
func savePassiveLog(activity string, systemUsage map[string]interface{}) {
	err := ensureLogFolder()
	if err != nil {
		log.Printf("Error ensuring log folder: %v\n", err)
		return
	}

	fileName := filepath.Join(logFolder, "passive_log_ServerNode.csv")

	// Check if the CSV file exists, and if not, create it and add headers
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		file, err := os.Create(fileName)
		if err != nil {
			log.Printf("Error creating passive log CSV file: %v\n", err)
			return
		}
		defer file.Close()

		// Write header row
		headers := "Timestamp,Activity,MemoryTotal,MemoryUsed,MemoryUsedPercent,CPUUsage,LoadAvg,Uptime\n"
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

	logEntry := fmt.Sprintf(
		"%s,%s,%.2f,%.2f,%.2f,%.2f,%.2f,%d\n",
		time.Now().Format("2006-01-02T15:04:05-07:00"),
		activity,
		systemUsage["Memory Total"],
		systemUsage["Memory Used"],
		systemUsage["Memory Used %"],
		systemUsage["CPU Usage %"],
		systemUsage["Load Average (1m)"],
		systemUsage["Uptime"],
	)
	if _, err := file.WriteString(logEntry); err != nil {
		log.Printf("Error writing to passive log CSV file: %v\n", err)
	}
}



// Ensure the uploads folder exists
func ensureUploadsFolder() error {
	if _, err := os.Stat(logFolder); os.IsNotExist(err) {
		return os.Mkdir(logFolder, 0755)
	}
	return nil
}

// Handler for file/image upload
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// Limit the size of incoming requests
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // Limit to 10MB
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "File is too large", http.StatusBadRequest)
		return
	}

	// Retrieve the uploaded file
	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Ensure the uploads folder exists
	if err := ensureUploadsFolder(); err != nil {
		http.Error(w, "Error ensuring uploads folder", http.StatusInternalServerError)
		return
	}

	// Create a new file in the uploads folder
	filePath := filepath.Join(logFolder, handler.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy the uploaded file to the destination
	if _, err := ioutil.ReadAll(file); err != nil {
		http.Error(w, "Error reading the file", http.StatusInternalServerError)
		return
	}

	_, err = dst.Write([]byte{})
	if err != nil {
		http.Error(w, "Error writing the file", http.StatusInternalServerError)
		return
	}

	log.Printf("File uploaded successfully: %s\n", filePath)

	// Send a success response
	response := map[string]string{
		"status":  "success",
		"message": "File uploaded successfully",
		"path":    filePath,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
		savePassiveLog("Node registered with main server", nil)
	} else {
		log.Printf("Failed to register node. Status code: %d\n", resp.StatusCode)
		savePassiveLog("Node registration failed", nil)
	}
}

// Handler for health check endpoint
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	savePassiveLog("Health check received", nil)
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
	savePassiveLog("Request received and processed", usageData)
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
	mainServerURL := "https://13f0-2409-40c2-1161-f106-7915-c32-dfbb-67f9.ngrok-free.app" // Replace with actual main server URL

	// Self-register with the main server
	selfRegister(mainServerURL, serverNode)

	// Set up HTTP server
	http.HandleFunc("/receive", handleRequest)
	http.HandleFunc("/health", healthCheckHandler)
    http.HandleFunc("/upload", uploadHandler)


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
