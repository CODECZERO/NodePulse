package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "time"
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

    // Construct the URL to send messages
    messageURL := fmt.Sprintf("http://%s:%s/receive", nearestNode.IPAddress, nearestNode.Port)
    log.Printf("Connecting to server node at: %s", messageURL)

    // Start sending messages
    sendMessages(messageURL)
}

func sendMessages(url string) {
    message := map[string]string{
        "message": "Hello there",
    }

    for {
        // Encode the message into JSON format
        jsonData, err := json.Marshal(message)
        if err != nil {
            log.Printf("Error encoding message: %v", err)
            time.Sleep(5 * time.Second) // Wait before retrying
            continue
        }

        // Make an HTTP POST request to the server node
        resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
        if err != nil {
            log.Printf("Error making HTTP POST request: %v", err)
            time.Sleep(5 * time.Second) // Wait before retrying
            continue
        }
        defer resp.Body.Close()

        // Read the raw response body
        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            log.Printf("Error reading response body: %v", err)
            time.Sleep(5 * time.Second) // Wait before retrying
            continue
        }

        // Log the raw response body
        log.Printf("Raw response body: %s", body)

        // Check for non-200 HTTP status codes
        if resp.StatusCode != http.StatusOK {
            log.Printf("Unexpected status code: %d", resp.StatusCode)
            time.Sleep(5 * time.Second) // Wait before retrying
            continue
        }

        // Decode the response body into a map
        var result map[string]interface{}
        if err := json.Unmarshal(body, &result); err != nil {
            log.Printf("Error decoding response body: %v", err)
            time.Sleep(5 * time.Second) // Wait before retrying
            continue
        }

        log.Printf("Server response: %v", result)

        // Wait for a short period before sending the next message
        time.Sleep(1 * time.Second)
    }
}