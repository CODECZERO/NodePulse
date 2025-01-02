package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sync"
	"time"
	"log"

	"github.com/rs/cors"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/load"
)

// Node structure for storing node details
type Node struct {
	ID        string  `json:"id"`
	IPAddress string  `json:"ip_address"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Status    string  `json:"status"`
	Port      string  `json:"port"`
}

var (
	nodes       = make(map[string]Node) // Store nodes in memory
	mutex       = &sync.Mutex{}         // Mutex for synchronizing access to nodes
	clientCount = 0                     // Global counter for connected clients
	clientMutex = &sync.Mutex{}         // Mutex for synchronizing access to clientCount
)

// Register Node Handler
func registerNodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var node Node
	err := json.NewDecoder(r.Body).Decode(&node)
	if err != nil || node.ID == "" || node.IPAddress == "" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Add node to the map
	mutex.Lock()
	nodes[node.ID] = node
	mutex.Unlock()

	// Log to active log
	logToActiveLog("Node registered", node)

	// Respond with a success message
	response := map[string]string{
		"message": "Node registered successfully",
		"node_id": node.ID,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	fmt.Printf("Node registered: %+v\n", node)
}

// Find the nearest node for a client
func findNearestNode(clientLat, clientLon float64) Node {
	var nearest Node
	minDistance := math.MaxFloat64

	fmt.Println(nearest)

	mutex.Lock()
	defer mutex.Unlock()

	for _, node := range nodes {
		if node.Status != "active" {
			continue
		}
		distance := calculateDistance(clientLat, clientLon, node.Latitude, node.Longitude)
		if distance < minDistance {
			minDistance = distance
			nearest = node
		}
	}

	return nearest
}

// Distance calculation between two geo-coordinates
func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth's radius in km
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	lat1 = lat1 * math.Pi / 180
	lat2 = lat2 * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// Collect system metrics and log data
func collectSystemMetrics() (map[string]interface{}, error) {
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

	powerConsumption := cpuUsage[0] // Assume power consumption correlates with CPU usage (in a very simplified manner)

	metrics := map[string]interface{}{
		"Memory Used %":              memoryStats.UsedPercent,
		"CPU Usage %":                cpuUsage[0],
		"Load Average":               loadStats.Load1,
		"Power Consumption (estimate)": powerConsumption, // in percentage
	}

	// Log to passive log
	logToPassiveLog("System Metrics Collected", metrics)

	return metrics, nil
}

// Save metrics to the active log
func logToActiveLog(action string, details interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	file, err := os.OpenFile("active_log_MainServer.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening active log file: %v\n", err)
		return
	}
	defer file.Close()

	logEntry := fmt.Sprintf("[%s] ACTION: %s, DETAILS: %+v\n", timestamp, action, details)
	if _, err := file.WriteString(logEntry); err != nil {
		log.Printf("Error writing to active log file: %v\n", err)
	}
}

// Save metrics to the passive log
func logToPassiveLog(action string, details interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	file, err := os.OpenFile("passive_log_MainServer.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening passive log file: %v\n", err)
		return
	}
	defer file.Close()

	logEntry := fmt.Sprintf("[%s] ACTION: %s, DETAILS: %+v\n", timestamp, action, details)
	if _, err := file.WriteString(logEntry); err != nil {
		log.Printf("Error writing to passive log file: %v\n", err)
	}
}

// Redirect Client Handler (find nearest node and notify)
func redirectClientHandler(w http.ResponseWriter, r *http.Request) {
	clientLat := r.URL.Query().Get("lat")
	clientLon := r.URL.Query().Get("lon")

	if clientLat == "" || clientLon == "" {
		http.Error(w, "Missing client location parameters", http.StatusBadRequest)
		return
	}

	var lat, lon float64
	_, err := fmt.Sscanf(clientLat, "%f", &lat)
	if err != nil {
		http.Error(w, "Invalid latitude value", http.StatusBadRequest)
		return
	}
	_, err = fmt.Sscanf(clientLon, "%f", &lon)
	if err != nil {
		http.Error(w, "Invalid longitude value", http.StatusBadRequest)
		return
	}

	// Find the nearest node
	nearestNode := findNearestNode(lat, lon)
	if nearestNode.ID == "" {
		http.Error(w, "No active nodes found", http.StatusInternalServerError)
		return
	}
	fmt.Println(nearestNode)
	// Collect system metrics
	metrics, err := collectSystemMetrics()
	if err != nil {
		log.Printf("Error collecting system metrics: %v\n", err)
	} else {
		// Save metrics to passive log
		logToPassiveLog("System Metrics Collected", metrics)
	}

	// Respond with the nearest node information
	response := map[string]string{
		"nearest_node_id":   nearestNode.ID,
		"nearest_node_ip":   nearestNode.IPAddress,
		"nearest_node_port": nearestNode.Port,
		"nearest_node_lat":  fmt.Sprintf("%f", nearestNode.Latitude),
		"nearest_node_lon":  fmt.Sprintf("%f", nearestNode.Longitude),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	// Notify the nearest node asynchronously
	go func() {
		sendMessageToNode(nearestNode, "Client redirected to your server")
	}()
}

// Send a message to a specific server node
func sendMessageToNode(node Node, message string) {
	// Prepare the message payload
	msg := map[string]string{
		"node_id": node.ID,
		"message": message,
	}

	// Marshal the message into JSON
	data, err := json.Marshal(msg)
	if err != nil {
		fmt.Println("Error marshalling message:", err)
		return
	}

	// Construct the node's URL
	url := fmt.Sprintf("%s", node.IPAddress)
	fmt.Printf("Sending message to node: %s\n", url)

	// Send the message to the server node
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		fmt.Println("Error sending message to node:", err)
		return
	}
	defer resp.Body.Close()

	// Log the response from the server node
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response from node: %s\n", respBody)
}

// Long Polling Handler
func longPollHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate a long polling response
	time.Sleep(10 * time.Second) // Simulate a delay

	response := map[string]string{
		"status":  "ok",
		"message": "Long polling response",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Receive Handler
func receiveHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate processing the request
	response := map[string]string{
		"status":  "ok",
		"message": "Request received and processed",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	// Initialize CORS settings
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"}, // Allow all origins
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
	})

	// Register handlers
	http.HandleFunc("/register-node", registerNodeHandler)
	http.HandleFunc("/redirect-client", redirectClientHandler)
	http.HandleFunc("/long-poll", longPollHandler)
	http.HandleFunc("/receive", receiveHandler)

	// Start the main server
	fmt.Println("Main server is running on port 8080...")
	if err := http.ListenAndServe(":8080", corsHandler.Handler(http.DefaultServeMux)); err != nil {
		fmt.Println("Error starting server:", err)
	}
}
