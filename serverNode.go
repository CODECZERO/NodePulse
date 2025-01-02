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
	"io"
	
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
func getPublicIP() string {
	// Channel to receive the IP address
	ipChannel := make(chan string)

	go func() {
		resp, err := http.Get("https://api.ipify.org?format=text")
		if err != nil {
			log.Printf("Error fetching public IP: %v\n", err)
			ipChannel <- "" // Send an empty string if there is an error
			return
		}
		defer resp.Body.Close()

		ip, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading IP response: %v\n", err)
			ipChannel <- "" // Send an empty string if there is an error
			return
		}

		ipChannel <- string(ip) // Send the fetched IP to the channel
	}()

	// Wait for the result from the channel
	return <-ipChannel
}


func getNgrokPublicURL() (string, error) {
	// Create a channel to receive the result from the goroutine
	resultChan := make(chan string)
	errorChan := make(chan error)

	go func() {
		var ngrokURL string
		// Start Ngrok
		// Fetch the public URL from Ngrok API
		urlResp, err := http.Get("http://localhost:4040/api/tunnels")
		if err != nil {
			log.Printf("Error fetching Ngrok public URL: %v\n", err)
			errorChan <- err
			return
		}
		defer urlResp.Body.Close()

		// Read the response body
		body, err := ioutil.ReadAll(urlResp.Body)
		if err != nil {
			log.Printf("Error reading Ngrok API response: %v\n", err)
			errorChan <- err
			return
		}

		// Parse the response body to extract the public URL (assumes Ngrok is running on localhost:4040)
		var ngrokAPIResponse struct {
			Tunnels []struct {
				PublicURL string `json:"public_url"`
			} `json:"tunnels"`
		}

		err = json.Unmarshal(body, &ngrokAPIResponse)
		if err != nil {
			log.Printf("Error parsing Ngrok API response: %v\n", err)
			errorChan <- err
			return
		}

		if len(ngrokAPIResponse.Tunnels) == 0 {
			log.Printf("No tunnels found in Ngrok response\n")
			errorChan <- fmt.Errorf("no tunnels found")
			return
		}

		// Set the Ngrok public URL
		ngrokURL = ngrokAPIResponse.Tunnels[0].PublicURL
		log.Printf("Ngrok public URL: %s\n", ngrokURL)

		// Send the result back to the channel
		resultChan <- ngrokURL
	}()

	// Wait for the result from the goroutine
	select {
	case ngrokURL := <-resultChan:
		return ngrokURL, nil
	case err := <-errorChan:
		return "", err
	}
}

// Function to get geolocation using an external API
func getGeoLocation(ip string) (float64, float64, error) {
	// Create channels for receiving the result and error
	resultChan := make(chan struct {
		Lat float64
		Lon float64
	}, 1)
	errorChan := make(chan error, 1)

	go func() {
		// This is the goroutine to get geolocation based on IP
		geoAPI := fmt.Sprintf("http://ip-api.com/json/%s", ip)
		resp, err := http.Get(geoAPI)
		if err != nil {
			errorChan <- err
			return
		}
		defer resp.Body.Close()

		// Decode the JSON response into the geoData struct
		var geoData struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		}
		err = json.NewDecoder(resp.Body).Decode(&geoData)
		if err != nil {
			errorChan <- err
			return
		}

		// Send the results to the resultChan
		resultChan <- struct {
			Lat float64
			Lon float64
		}{Lat: geoData.Lat, Lon: geoData.Lon}
	}()

	// Wait for the result or error from the goroutine
	select {
	case geo := <-resultChan:
		return geo.Lat, geo.Lon, nil
	case err := <-errorChan:
		return 0, 0, err
	}
}
// Function to get local IP address of the machine
func getLocalIPAddress() (string, error) {
	// Create a channel to receive the result or error
	resultChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	go func() {
		// This is the goroutine to find the local IP address
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			errorChan <- err
			return
		}

		// Loop through addresses and find a non-loopback IPv4 address
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
				resultChan <- ipNet.IP.String()
				return
			}
		}

		// If no valid IP address is found
		errorChan <- fmt.Errorf("no IP address found")
	}()

	// Wait for the result or error to be received
	select {
	case ip := <-resultChan:
		return ip, nil
	case err := <-errorChan:
		return "", err
	}
}

func measureLatency(target string) (float64, error) {
	// Create a channel for error handling and latency result
	latencyChan := make(chan float64)
	errChan := make(chan error)

	// Start the latency measurement in a separate goroutine
	go func() {
		start := time.Now()
		resp, err := http.Get(target)
		if err != nil {
			errChan <- err
			return
		}
		defer resp.Body.Close()

		latency := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		latencyChan <- latency
	}()

	// Wait for the result or error from the goroutine
	select {
	case latency := <-latencyChan:
		return latency, nil
	case err := <-errChan:
		return 0, err
	}
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

	usageData := map[string]interface{}{
		"Memory Total":      memoryStats.Total / (1024 * 1024),
		"Memory Used":       memoryStats.Used / (1024 * 1024),
		"Memory Used %":     memoryStats.UsedPercent,
		"CPU Usage %":       cpuUsage[0],
		"Load Average (1m)": loadStats.Load1,
		"Uptime":            hostInfo.Uptime,
	}

	return usageData, nil
}

// Function to save system and client data to a CSV file (active log)
func saveActiveLog(clientIP string, clientLatitude, clientLongitude float64, nodeLatitude, nodeLongitude float64, latency float64, timestamp string, clientData string, systemUsage map[string]interface{}) {
	// Create a channel for error handling
	errChan := make(chan error)

	// Create a goroutine for ensuring the log folder exists
	go func() {
		err := ensureLogFolder()
		if err != nil {
			errChan <- fmt.Errorf("Error ensuring log folder: %v", err)
			return
		}
		errChan <- nil
	}()

	// Wait for folder creation result
	if err := <-errChan; err != nil {
		log.Printf("Error ensuring log folder: %v\n", err)
		return
	}

	// File name for logging
	fileName := filepath.Join(logFolder, "active_log_ServerNode.csv")

	// Create a channel for file handling errors
	fileErrChan := make(chan error)

	// Check if the CSV file exists, and if not, create it and add headers asynchronously
	go func() {
		if _, err := os.Stat(fileName); os.IsNotExist(err) {
			// Create the file and write headers if it doesn't exist
			file, err := os.Create(fileName)
			if err != nil {
				fileErrChan <- fmt.Errorf("Error creating active log CSV file: %v", err)
				return
			}
			defer file.Close()

			// Write header row
			headers := "ClientIP,ClientLatitude,ClientLongitude,NodeLatitude,NodeLongitude,Latency,Timestamp,ClientData,MemoryTotal,MemoryUsed,MemoryUsedPercent,CPUUsage,LoadAvg,Uptime\n"
			if _, err := file.WriteString(headers); err != nil {
				fileErrChan <- fmt.Errorf("Error writing headers to active log CSV file: %v", err)
				return
			}
		}
		fileErrChan <- nil
	}()

	// Wait for file creation and header-writing result
	if err := <-fileErrChan; err != nil {
		log.Printf("%v\n", err)
		return
	}

	// Open the file in append mode asynchronously
	go func() {
		file, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			errChan <- fmt.Errorf("Error opening active log CSV file: %v", err)
			return
		}
		defer file.Close()

		// Prepare the log entry
		logEntry := fmt.Sprintf(
			"%s,%.6f,%.6f,%.6f,%.6f,%.3f,%s,%s,%.2f,%.2f,%.2f,%.2f,%.2f,%d\n",
			clientIP,
			clientLatitude,
			clientLongitude,
			nodeLatitude, // Server Latitude
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
		)

		// Write the log entry to the file
		if _, err := file.WriteString(logEntry); err != nil {
			errChan <- fmt.Errorf("Error writing to active log CSV file: %v", err)
			return
		}
		errChan <- nil
	}()

	// Wait for the final file writing result
	if err := <-errChan; err != nil {
		log.Printf("Error writing to active log: %v\n", err)
		return
	}
}


// Function to save passive logs (background server operations)
func savePassiveLog(activity string, systemUsage map[string]interface{}) {
	// Create a channel for error handling
	errChan := make(chan error)

	// Create a goroutine for ensuring the log folder exists
	go func() {
		err := ensureLogFolder()
		if err != nil {
			errChan <- fmt.Errorf("Error ensuring log folder: %v", err)
			return
		}
		errChan <- nil
	}()

	// Wait for folder creation result
	if err := <-errChan; err != nil {
		log.Printf("Error ensuring log folder: %v\n", err)
		return
	}

	fileName := filepath.Join(logFolder, "passive_log_ServerNode.csv")

	// Create a channel for file handling errors
	fileErrChan := make(chan error)

	// Check if the CSV file exists, and if not, create it and add headers asynchronously
	go func() {
		if _, err := os.Stat(fileName); os.IsNotExist(err) {
			// Create the file and write headers if it doesn't exist
			file, err := os.Create(fileName)
			if err != nil {
				fileErrChan <- fmt.Errorf("Error creating passive log CSV file: %v", err)
				return
			}
			defer file.Close()

			// Write header row
			headers := "Timestamp,Activity,MemoryTotal,MemoryUsed,MemoryUsedPercent,CPUUsage,LoadAvg,Uptime\n"
			if _, err := file.WriteString(headers); err != nil {
				fileErrChan <- fmt.Errorf("Error writing headers to passive log CSV file: %v", err)
				return
			}
		}
		fileErrChan <- nil
	}()

	// Wait for file creation and header-writing result
	if err := <-fileErrChan; err != nil {
		log.Printf("%v\n", err)
		return
	}

	// Open the file in append mode asynchronously
	go func() {
		file, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			errChan <- fmt.Errorf("Error opening passive log CSV file: %v", err)
			return
		}
		defer file.Close()

		// Prepare the log entry
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

		// Write the log entry to the file
		if _, err := file.WriteString(logEntry); err != nil {
			errChan <- fmt.Errorf("Error writing to passive log CSV file: %v", err)
			return
		}
		errChan <- nil
	}()

	// Wait for the final file writing result
	if err := <-errChan; err != nil {
		log.Printf("Error writing to passive log: %v\n", err)
		return
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
	// Limit the size of incoming requests to 10MB
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
		http.Error(w, "Error creating the file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy the uploaded file content to the destination file
	_, err = io.Copy(dst, file)

	if err != nil {
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}

	// Log success
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

	// Create channels for geolocation, system usage, and latency
	geoLocationChan := make(chan [2]float64)
	usageDataChan := make(chan map[string]interface{})
	latencyChan := make(chan float64)
	errorChan := make(chan error)

	// Fetch geolocation asynchronously
	go func() {
		clientLatitude, clientLongitude, err := getGeoLocation(clientIP)
		if err != nil {
			log.Printf("Error fetching client geolocation: %v\n", err)
			errorChan <- err
			return
		}
		geoLocationChan <- [2]float64{clientLatitude, clientLongitude}
	}()

	// Capture system usage data asynchronously
	go func() {
		usageData, err := captureSystemUsage()
		if err != nil {
			log.Printf("Error capturing system usage data: %v\n", err)
			errorChan <- err
			return
		}
		usageDataChan <- usageData
	}()

	// Measure latency asynchronously
	go func() {
		latency, err := measureLatency("https://google.com")
		if err != nil {
			log.Printf("Error measuring latency: %v\n", err)
			latencyChan <- -1 // Indicate error
			return
		}
		latencyChan <- latency
	}()

	// Wait for the results from the channels
	var clientLatitude, clientLongitude float64
	select {
	case geoLocation := <-geoLocationChan:
		// Use the geolocation data
		clientLatitude, clientLongitude = geoLocation[0], geoLocation[1]
		log.Printf("Client geolocation: %.6f, %.6f\n", clientLatitude, clientLongitude)

	case err := <-errorChan:
		// Handle error if geolocation, usage, or latency fetching fails
		http.Error(w, fmt.Sprintf("Error processing request: %v", err), http.StatusInternalServerError)
		return
	}

	var usageData map[string]interface{}
	select {
	case usageData = <-usageDataChan:
		// Log usage data if available
		log.Printf("System usage: %v\n", usageData)
	case err := <-errorChan:
		http.Error(w, fmt.Sprintf("Error processing request: %v", err), http.StatusInternalServerError)
		return
	}

	var latency float64
	select {
	case latency = <-latencyChan:
		log.Printf("Measured latency: %.2f ms\n", latency)
	case err := <-errorChan:
		http.Error(w, fmt.Sprintf("Error processing request: %v", err), http.StatusInternalServerError)
		return
	}

	// Get the current timestamp
	timestamp := time.Now().Format("2006-01-02T15:04:05-07:00")

	// Prepare client data
	clientData := string(requestBody)

	// Save data to active log (interaction)
	saveActiveLog(clientIP, clientLatitude, clientLongitude, serverNode.Latitude, serverNode.Longitude, latency, timestamp, clientData, usageData)

	// Prepare the JSON response
	response := map[string]string{
		"status":  "ok",
		"message": "Request received and processed",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	// Save passive log for background operation
	go savePassiveLog("Request received and processed", usageData)
}
func main() {
	log.Println("Starting server node...")

	// Create channels for handling asynchronous tasks
	geolocationChan := make(chan struct {
		latitude, longitude float64
	}, 1)
	publicIPChan := make(chan string, 1)
	localIPChan := make(chan string, 1)
	ngrokURLChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	// Goroutine to fetch public IP address
	go func() {
		publicIP := getPublicIP()
		publicIPChan <- publicIP
	}()

	// Goroutine to fetch geolocation data
	go func() {
		publicIP := <-publicIPChan
		latitude, longitude, err := getGeoLocation(publicIP)
		if err != nil {
			errorChan <- err
			return
		}
		geolocationChan <- struct {
			latitude, longitude float64
		}{latitude, longitude}
	}()

	// Goroutine to fetch local IP address
	go func() {
		localIP, err := getLocalIPAddress()
		if err != nil {
			errorChan <- err
			return
		}
		localIPChan <- localIP
	}()

	// Goroutine to fetch ngrok public URL
	go func() {
		ngrokPublicURL, err := getNgrokPublicURL()
		if err != nil {
			errorChan <- err
			return
		}
		ngrokURLChan <- ngrokPublicURL
	}()

	// Declare latitude and longitude variables before select block
	var latitude, longitude float64
	var publicIP, localIP, ngrokPublicURL string

	// Wait for all tasks to complete or for an error
	select {
	case geolocation := <-geolocationChan:
		// Unpacking latitude and longitude from geolocation
		latitude = geolocation.latitude
		longitude = geolocation.longitude
		log.Printf("Geolocation fetched: Latitude: %.6f, Longitude: %.6f\n", latitude, longitude)
	case err := <-errorChan:
		log.Println("Error:", err)
		return
	}

	// Get other necessary values
	localIP = <-localIPChan
	log.Println("Local IP Address:", localIP)
	log.Println("public IP Address:", publicIP)

	ngrokPublicURL = <-ngrokURLChan
	log.Println("Ngrok Public URL:", ngrokPublicURL)

	// Generate unique node ID
	nodeID := uuid.New().String()

	// Node information
	port := "8081"
	serverNode := Node{
		ID:        nodeID,
		IPAddress: ngrokPublicURL,
		Latitude:  latitude,
		Longitude: longitude,
		Port:      port,
		Status:    "active",
	}

	// Main server URL
	mainServerURL := "https://89aa-2409-40c2-116b-abb-8bcc-8f3e-2a0-ab10.ngrok-free.app" // Replace with actual main server URL

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
