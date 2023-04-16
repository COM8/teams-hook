package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	connectionMutex sync.Mutex
	connections     []*websocket.Conn

	// Flags
	portFlag        int    = 1997
	certPathFlag    string = "cert.crt"
	keyPathFlag     string = "cert.key"
	accessTokenFlag string = ""
)

func addConnection(connection *websocket.Conn) {
	connectionMutex.Lock()
	connections = append(connections, connection)
	connectionMutex.Unlock()
}

func removeConnection(connection *websocket.Conn) {
	connectionMutex.Lock()
	var index int = -1
	for i := 0; i < (len(connections) - 1); i++ {
		if connections[i] == connection {
			index = i
			break
		}
	}

	if index < 0 {
		log.Println("Failed to remove connection. Connection not found!")
	} else {
		connections = append(connections[:index], connections[index+1:]...)
	}
	connectionMutex.Unlock()
}

func wsAuthenticate(connection *websocket.Conn) bool {
	_, p, err := connection.ReadMessage()
	if err != nil {
		log.Println(err)
		return false
	}
	authenticated := string(p) == accessTokenFlag
	if authenticated {
		err = connection.WriteMessage(websocket.TextMessage, []byte("{ \"auth\": true }"))
		if err != nil {
			log.Println(err)
			return false
		}
	} else {
		err = connection.WriteMessage(websocket.TextMessage, []byte("{ \"auth\": false }"))
		if err != nil {
			log.Println(err)
			return false
		}
	}
	return authenticated
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer connection.Close()

	// Authenticate
	if !wsAuthenticate(connection) {
		return
	}

	// Add connection
	addConnection(connection)
	log.Println("New connection added.")

	for {
		if _, _, err := connection.NextReader(); err != nil {
			connection.Close()
			break
		}
	}

	// Remove connection
	removeConnection(connection)
	log.Println("Connection removed.")
}

func notifySockets(msg []byte) {
	connectionMutex.Lock()
	for i := 0; i <= (len(connections) - 1); i++ {
		err := connections[i].WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			log.Println(err)
			break
		}
	}
	log.Printf("Send message of length %d to %d websockets", len(msg), len(connections))
	connectionMutex.Unlock()
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		log.Printf("Received POST request with body: '%s'\n", string(body))

		queryParams := r.URL.Query()
		validationToken := queryParams.Get("validationToken")
		if validationToken == "" {
			notifySockets(body)
			return
		} else {
			log.Printf("Validation token found! Responding...")
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(validationToken))
		}
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func main() {
	// Flags
	flag.StringVar(&accessTokenFlag, "token", "", "Access token for validating websocket access")
	flag.StringVar(&keyPathFlag, "key", "cert.key", "TLS key path")
	flag.StringVar(&certPathFlag, "cert", "cert.crt", "TLS cert path")
	flag.IntVar(&portFlag, "port", 1997, "The port for the webserver and websocket are running on")
	help := flag.Bool("help", false, "Show help")

	// Parse the flag
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if len(accessTokenFlag) <= 0 {
		log.Printf("Flag '--token <some_access_token>' required!")
		flag.Usage()
		os.Exit(1)
	}

	// Start
	log.Println("Starting Teams Hook...")
	http.HandleFunc("/", webhookHandler)

	log.Println("Staring websocket...")
	http.HandleFunc("/ws", wsHandler)
	log.Println("Websocket started.")

	log.Println("Teams Hook started.")
	err := http.ListenAndServeTLS(fmt.Sprintf(":%d", portFlag), certPathFlag, keyPathFlag, nil)
	if err != nil {
		log.Fatal("ListenAndServeTLS: ", err)
	}
	log.Println("Teams Hook stopped.")
}
