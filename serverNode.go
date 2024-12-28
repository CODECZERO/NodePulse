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

    "github.com/google/uuid"
    "github.com/rs/cors"
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
    } else {
        log.Printf("Failed to register node. Status code: %d\n", resp.StatusCode)
    }
}

// Handler for health check endpoint
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
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

    log.Printf("Received data from client IP %s: %s\n", clientIP, string(requestBody))

    // Prepare the JSON response
    response := map[string]string{
        "status":  "ok",
        "message": "Request received and processed",
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
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