package main

import (
	b64 "encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sync"

	"github.com/gorilla/websocket"
)

type wsConnection struct {
	connection *websocket.Conn
	id         int64
}

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	connectionMutex sync.Mutex
	connections     []*wsConnection

	// Flags
	portFlag        int    = 1997
	certPathFlag    string = "cert.crt"
	keyPathFlag     string = "cert.key"
	accessTokenFlag string = ""

	connId int64 = 0
)

func addConnection(connection *wsConnection) {
	connectionMutex.Lock()

	connId += 1
	connection.id = connId
	connections = append(connections, connection)

	connectionMutex.Unlock()
}

func removeConnection(connection *wsConnection) {
	// Only in case the id is >= we added the connection to the list
	if connection.id <= 0 {
		return
	}

	connectionMutex.Lock()
	var index int = -1
	for i := 0; i < len(connections); i++ {
		if connections[i].id == connection.id {
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
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	wsConn := wsConnection{connection, 0}

	defer connection.Close()
	connection.SetCloseHandler(func(code int, text string) error {
		if len(text) <= 0 {
			log.Println("Socket closed with code", code, ".")
		} else {
			log.Println("Socket closed with code", code, "(", text, ").")
		}

		// Remove connection
		removeConnection(&wsConn)
		log.Println("Connection removed.", len(connections), "connections left.")
		return nil
	})

	// Authenticate
	if !wsAuthenticate(connection) {
		return
	}

	// Add connection
	addConnection(&wsConn)
	log.Println("New connection added (total", len(connections), ").")

	for {
		if _, _, err := connection.NextReader(); err != nil {
			connection.Close()
			break
		}
	}

	// Connection will be removed once closed
}

func notifySockets(msg []byte) {
	connectionMutex.Lock()
	for i := 0; i <= (len(connections) - 1); i++ {
		err := connections[i].connection.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			log.Println(err)
		}
	}
	log.Printf("Send message of length %d to %d websockets", len(msg), len(connections))
	connectionMutex.Unlock()
}

func parseMsg(msg string) map[string]interface{} {
	// Replace line breaks:
	re := regexp.MustCompile(`\r?\n`)
	msg = re.ReplaceAllString(msg, " ")

	// Extract JSON:
	r := regexp.MustCompile(`^((\d*:)+)(\{.+\})$`)
	matches := r.FindStringSubmatch(msg)
	if len(matches) == 4 {
		var jsonData map[string]interface{}
		var m = matches[3]
		err := json.Unmarshal([]byte(m), &jsonData)
		if err != nil {
			log.Println(err)
			return make(map[string]interface{})
		}
		return jsonData
	}

	return make(map[string]interface{})
}

func parseExtractEvent(msg string) (map[string]interface{}, bool) {
	// Parse data:
	jData := parseMsg(string(msg))
	if len(jData) <= 0 {
		log.Println("No valid JSON data received.")
		return make(map[string]interface{}), false
	}

	// Extract event data:
	val, ok := jData["body"]
	if ok {
		if data, ok := val.(string); ok {
			var eventData map[string]interface{}
			err := json.Unmarshal([]byte(data), &eventData)
			if err != nil {
				log.Println(err)
				return make(map[string]interface{}), false
			}

			// Incoming calls:
			val, ok = eventData["gp"]
			if ok {
				if data, ok = val.(string); ok {
					gp, err := b64.StdEncoding.DecodeString(data)
					if err == nil {
						var eventPayload map[string]interface{}
						err = json.Unmarshal([]byte(gp), &eventPayload)
						if err == nil {
							return eventPayload, true
						}
					}
				}
			} else {
				// Call ended:
				val, ok = eventData["callEnd"]
				log.Println("VAL: ", val)
				if ok {
					if data, ok = val.(string); ok {
						log.Println("DATA: ", val)
						var eventPayload map[string]interface{}
						err = json.Unmarshal([]byte(data), &eventPayload)
						if err == nil {
							return eventPayload, true
						}
					}
				}
			}
		}
	}
	return make(map[string]interface{}), false
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		// Check if the request is authenticated:
		authHeader := r.URL.Query().Get("auth") // Send the auth via query parameter to prevent cors issues
		if authHeader != accessTokenFlag {
			log.Printf("No valid auth token found! Received '%s' inside the 'auth' header.", authHeader)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ \"auth\": false }"))
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// Parse the body:
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		log.Printf("Received POST request with body: '%s'\n", string(body))

		// Extract event data:
		eventData, ok := parseExtractEvent(string(body))
		if ok {
			// Convert valid JSON back to a string:
			out, _ := json.Marshal(&eventData)
			// Notify all subscribers:
			notifySockets(out)
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
