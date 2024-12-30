package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
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

    powerUsage, err := getPowerUsage() // Capture power usage
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
        "Power Usage (W)":   powerUsage, // Add power usage data
    }

    return usageData, nil
}

// Function to get actual power usage from the Linux sysfs interface
func getPowerUsage() (float64, error) {
    powerPath := "/sys/class/power_supply/BAT0/power_now"
    if _, err := os.Stat(powerPath); os.IsNotExist(err) {
        return 0, fmt.Errorf("power usage file not found")
    }

    data, err := ioutil.ReadFile(powerPath)
    if err != nil {
        return 0, err
    }

    var powerUsage float64
    fmt.Sscanf(string(data), "%f", &powerUsage)
    // Convert microWatts to Watts
    powerUsage /= 1e6

    return powerUsage, nil
}

// Function to ensure the log folder exists
func ensureLogFolderExists() error {
    folderName := "serverNodeData"

    if _, err := os.Stat(folderName); os.IsNotExist(err) {
        err := os.Mkdir(folderName, os.ModePerm)
        if err != nil {
            return fmt.Errorf("could not create log folder: %v", err)
        }
    }
    return nil
}

// Function to save system and client data to a CSV file (active log)
func saveActiveLog(clientIP string, clientLatitude, clientLongitude float64, nodeLatitude, nodeLongitude float64, latency float64, timestamp string, clientData string, systemUsage map[string]interface{}) {
    err := ensureLogFolderExists()
    if err != nil {
        log.Println("Error ensuring log folder exists:", err)
        return
    }

    fileName := "serverNodeData/active_log_ServerNode.csv"

    if _, err := os.Stat(fileName); os.IsNotExist(err) {
        file, err := os.Create(fileName)
        if err != nil {
            log.Printf("Error creating active log CSV file: %v\n", err)
            return
        }
        defer file.Close()

        headers := "ClientIP,ClientLatitude,ClientLongitude,NodeLatitude,NodeLongitude,Latency,Timestamp,ClientData,MemoryTotal,MemoryUsed,MemoryUsedPercent,CPUUsage,LoadAvg,Uptime,PowerUsage\n"
        if _, err := file.WriteString(headers); err != nil {
            log.Printf("Error writing headers to active log CSV file: %v\n", err)
        }
    }

    file, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        log.Printf("Error opening active log CSV file: %v\n", err)
        return
    }
    defer file.Close()

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
    w.Header().Set("Content-Type", "application/json")
    w.Write([]byte(`{"status":"active", "message":"Server is running"}`))
}

// Handler for file upload
func uploadFileHandler(w http.ResponseWriter, r *http.Request) {
    clientIP := r.RemoteAddr // Capture client IP address
    if r.Method == http.MethodPost && r.Header.Get("Content-Type")[:19] == "multipart/form-data" {
        err := r.ParseMultipartForm(10 << 20) // Limit file size to 10 MB
        if err != nil {
            http.Error(w, "Failed to parse form data", http.StatusBadRequest)
            return
        }

        file, _, err := r.FormFile("file")
        if err != nil {
            http.Error(w, "Failed to retrieve file", http.StatusBadRequest)
            return
        }
        defer file.Close()

        err = os.MkdirAll("uploads", os.ModePerm)
        if err != nil {
            http.Error(w, "Failed to create upload directory", http.StatusInternalServerError)
            return
        }

        outFile, err := os.Create("uploads/uploaded_image.jpg")
        if err != nil {
            http.Error(w, "Failed to create file on server", http.StatusInternalServerError)
            return
        }
        defer outFile.Close()

        _, err = io.Copy(outFile, file)
        if err != nil {
            http.Error(w, "Failed to save file", http.StatusInternalServerError)
            return
        }

        response := map[string]string{
            "status":  "ok",
            "message": "File uploaded successfully",
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
        log.Printf("Image uploaded from client: %s", clientIP)
    } else {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
    }
}

// Handler for receiving plain data or JSON data
func receiveDataHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodPost {
        var body map[string]interface{}
        decoder := json.NewDecoder(r.Body)
        err := decoder.Decode(&body)
        if err != nil {
            http.Error(w, "Failed to read data", http.StatusBadRequest)
            return
        }

        log.Println("Received data:", body)
        response := map[string]string{
            "status":  "ok",
            "message": "Data received successfully",
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
    } else {
        http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
    }
}

func main() {
    publicIP, err := getPublicIP()
    if err != nil {
        log.Fatal("Error getting public IP:", err)
    }

    lat, lon, err := getGeoLocation(publicIP)
    if err != nil {
        log.Fatal("Error getting geolocation:", err)
    }

    serverNode = Node{
        ID:        uuid.New().String(),
        IPAddress: publicIP,
        Latitude:  lat,
        Longitude: lon,
        Port:      "8080", // Assuming the server is running on port 8080
        Status:    "active",
    }

    go func() {
        // Start HTTP server with CORS enabled
        http.HandleFunc("/health", healthCheckHandler)
        http.HandleFunc("/upload", uploadFileHandler)    // Handle file uploads
        http.HandleFunc("/receive", receiveDataHandler)  // Handle plain or JSON data
        log.Println("Server started on port 8080...")
        log.Fatal(http.ListenAndServe(":8081", cors.Default().Handler(http.DefaultServeMux)))
    }()

    // Simulate the node registering itself with the main server after startup
    selfRegister("http://localhost:8080", serverNode)

    // Handle graceful shutdown
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    <-sigs
    log.Println("Server shutting down...")
}
