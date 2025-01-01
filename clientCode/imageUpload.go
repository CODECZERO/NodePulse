package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "mime/multipart"
    "net/http"
    "os"
   
)

type Node struct {
    ID        string  `json:"nearest_node_id"`
    IPAddress string  `json:"nearest_node_ip"`
    Latitude  float64 `json:"nearest_node_lat,string"`
    Longitude float64 `json:"nearest_node_lon,string"`
    Port      string  `json:"nearest_node_port"`
}

func main() {
    lat := 40.730610
    lon := -73.935242

    mainServerURL := fmt.Sprintf("http://localhost:8080/redirect-client?lat=%f&lon=%f", lat, lon)

    // Print the URL for debugging purposes
    log.Printf("Requesting nearest node from URL: %s", mainServerURL)

    // Make an HTTP GET request to the main server
    resp, err := http.Get(mainServerURL)
    if err != nil {
        log.Fatalf("Error making HTTP request: %v", err)
    }
    defer resp.Body.Close()

    // Check for non-200 HTTP status codes
    if resp.StatusCode != http.StatusOK {
        log.Fatalf("Unexpected status code: %d", resp.StatusCode)
    }

    // Decode the response body into the `Node` struct
    var nearestNode Node
    if err := json.NewDecoder(resp.Body).Decode(&nearestNode); err != nil {
        log.Fatalf("Error decoding response body: %v", err)
    }

    // Print the nearest node details for logging/debugging
    log.Printf("Redirected to nearest server node:\n"+
        "  ID: %s\n  IP: %s\n  Latitude: %.6f\n  Longitude: %.6f\n  Port: %s",
        nearestNode.ID, nearestNode.IPAddress, nearestNode.Latitude, nearestNode.Longitude, nearestNode.Port)

    // Construct the URL to upload a file
    uploadURL := fmt.Sprintf("%s/upload", nearestNode.IPAddress)
    log.Printf("Connecting to server node at: %s", uploadURL)

    // Upload a file to the server node
    filePath := "/home/mrrip/Pictures/Screenshot_2024-12-12_20_12_06.png" // Replace with the actual file path
    uploadFile(uploadURL, filePath)
}

func uploadFile(url string, filePath string) {
    // Open the file to be uploaded
    file, err := os.Open(filePath)
    if err != nil {
        log.Fatalf("Error opening file: %v", err)
    }
    defer file.Close()

    // Create a buffer to hold the multipart form data
    var requestBody bytes.Buffer
    writer := multipart.NewWriter(&requestBody)

    // Add the file to the form data
    part, err := writer.CreateFormFile("file", file.Name())
    if err != nil {
        log.Fatalf("Error creating form file: %v", err)
    }

    // Copy the file contents to the form data
    _, err = io.Copy(part, file)
    if err != nil {
        log.Fatalf("Error copying file contents: %v", err)
    }

    // Close the writer to finalize the form data
    err = writer.Close()
    if err != nil {
        log.Fatalf("Error closing multipart writer: %v", err)
    }

    // Make the HTTP POST request with the multipart form data
    req, err := http.NewRequest("POST", url, &requestBody)
    if err != nil {
        log.Fatalf("Error creating HTTP request: %v", err)
    }

    // Set the Content-Type to multipart/form-data
    req.Header.Set("Content-Type", writer.FormDataContentType())

    // Send the request to the server node
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Fatalf("Error making HTTP request: %v", err)
    }
    defer resp.Body.Close()

    // Read the response from the server
    responseBody, err := io.ReadAll(resp.Body)
    if err != nil {
        log.Fatalf("Error reading response body: %v", err)
    }

    // Log the server's response
    log.Printf("Server response: %s", string(responseBody))

    // Check for successful upload (200 OK)
    if resp.StatusCode != http.StatusOK {
        log.Fatalf("Failed to upload file. Status code: %d", resp.StatusCode)
    }

    log.Println("File uploaded successfully.")
}
