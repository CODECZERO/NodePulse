package main

import (
	"fmt"
	"io"
	"log"
	"net"
	
)

func main() {
	// Dynamically accept values for local and remote port forwarding
	localPort := "8081"
	remoteHost :="https://b95c-2409-40c2-1161-f106-7915-c32-dfbb-67f9.ngrok-free.app "
	remotePort := "80"

	if localPort == "" || remoteHost == "" || remotePort == "" {
		log.Fatal("Please set LOCAL_PORT, REMOTE_HOST, and REMOTE_PORT environment variables.")
	}

	// Create a listener for the local port
	localAddr := fmt.Sprintf("localhost:%s", localPort)
	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		log.Fatalf("Error starting listener on %s: %v", localAddr, err)
	}
	defer listener.Close()

	log.Printf("Listening on %s and forwarding to %s:%s", localAddr, remoteHost, remotePort)

	// Accept incoming connections and forward them
	for {
		// Wait for an incoming connection
		localConn, err := listener.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v", err)
			continue
		}

		// Forward the connection to the remote server
		go forwardConnection(localConn, remoteHost, remotePort)
	}
}

// forwardConnection handles the forwarding of data between local and remote connection
func forwardConnection(localConn net.Conn, remoteHost, remotePort string) {
	// Connect to the remote server
	remoteAddr := fmt.Sprintf("%s:%s", remoteHost, remotePort)
	remoteConn, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		log.Printf("Error connecting to remote server %s: %v", remoteAddr, err)
		localConn.Close()
		return
	}
	defer remoteConn.Close()

	// Forward data from local connection to remote server
	go forwardData(localConn, remoteConn)
	// Forward data from remote server to local connection
	go forwardData(remoteConn, localConn)
}

// forwardData forwards data from one connection to another
func forwardData(src, dest net.Conn) {
	_, err := io.Copy(dest, src)
	if err != nil {
		log.Printf("Error forwarding data: %v", err)
	}
}
