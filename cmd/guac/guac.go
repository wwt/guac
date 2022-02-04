package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/wwt/guac"
)

var tunnels map[string]guac.Tunnel

func main() {
	logrus.SetLevel(logrus.DebugLevel)

	// servlet := guac.NewServer(DemoDoConnect)
	wsServer := guac.NewWebsocketServer(DemoDoConnect)

	sessions := guac.NewMemorySessionStore()
	wsServer.OnConnect = sessions.Add
	wsServer.OnDisconnect = sessions.Delete

	tunnels = make(map[string]guac.Tunnel, 0)

	wsServer.OnConnectWs = func(s string, _ *websocket.Conn, _ *http.Request, t guac.Tunnel) {
		tunnels[s] = t
	}

	wsServer.OnDisconnectWs = func(s string, _ *websocket.Conn, _ *http.Request, _ guac.Tunnel) {
		delete(tunnels, s)
	}

	m := mux.NewRouter()
	// m.Handle("/", servlet)
	m.Handle("/websocket-tunnel", wsServer)

	m.HandleFunc("/api/session/tunnels/{tunnel}/streams/{stream}/{file}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", "attachment")
		t := mux.Vars(r)["tunnel"]

		tunnel, ok := tunnels[t]
		if !ok {
			w.Write([]byte("KO"))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		sit, ok := tunnel.(*guac.StreamInterceptingTunnel)
		if !ok {
			w.Write([]byte("Not supported"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		stream := mux.Vars(r)["stream"]

		streamIndex, err := strconv.Atoi(stream)
		if err != nil {
			w.Write([]byte("KO integer"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := sit.InterceptOutputStream(streamIndex, w); err != nil {
			w.Write([]byte("KO Intercepting output stream"))
		}
	}).Methods("GET")

	m.HandleFunc("/api/session/tunnels/{tunnel}/streams/{stream}/{file}", func(w http.ResponseWriter, r *http.Request) {
		// w.Header().Set("Content-Type", "application/json")
		t := mux.Vars(r)["tunnel"]
		tunnel, ok := tunnels[t]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("KO"))
			return
		}

		sit, ok := tunnel.(*guac.StreamInterceptingTunnel)
		if !ok {
			w.Write([]byte("Not supported"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		stream := mux.Vars(r)["stream"]

		streamIndex, err := strconv.Atoi(stream)
		if err != nil {
			w.Write([]byte("KO integer"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := sit.InterceptInputStream(streamIndex, r.Body); err != nil {
			w.Write([]byte("KO intercepting input stream"))
		}
	}).Methods("POST")

	m.HandleFunc("/api/session/tunnels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		t := []string{}
		for tun := range tunnels {
			t = append(t, tun)
		}

		if err := json.NewEncoder(w).Encode(t); err != nil {
			logrus.Error(err)
		}
	})

	logrus.Println("Serving on http://127.0.0.1:4567")

	s := &http.Server{
		Addr:           "0.0.0.0:4567",
		Handler:        m,
		ReadTimeout:    guac.SocketTimeout,
		WriteTimeout:   guac.SocketTimeout,
		MaxHeaderBytes: 1 << 20,
	}
	err := s.ListenAndServe()
	if err != nil {
		fmt.Println(err)
	}
}

// DemoDoConnect creates the tunnel to the remote machine (via guacd)
func DemoDoConnect(request *http.Request) (guac.Tunnel, error) {
	config := guac.NewGuacamoleConfiguration()

	var query url.Values
	if request.URL.RawQuery == "connect" {
		// http tunnel uses the body to pass parameters
		data, err := ioutil.ReadAll(request.Body)
		if err != nil {
			logrus.Error("Failed to read body ", err)
			return nil, err
		}
		_ = request.Body.Close()
		queryString := string(data)
		query, err = url.ParseQuery(queryString)
		if err != nil {
			logrus.Error("Failed to parse body query ", err)
			return nil, err
		}
		logrus.Debugln("body:", queryString, query)
	} else {
		query = request.URL.Query()
	}

	config.Protocol = query.Get("scheme")
	config.Parameters = map[string]string{
		"enable-sftp":   "true",
		"sftp-hostname": "198.18.251.1",
		"sftp-port":     "22",
	}
	for k, v := range query {
		config.Parameters[k] = v[0]
	}

	var err error
	if query.Get("width") != "" {
		config.OptimalScreenHeight, err = strconv.Atoi(query.Get("width"))
		if err != nil || config.OptimalScreenHeight == 0 {
			logrus.Error("Invalid height")
			config.OptimalScreenHeight = 600
		}
	}
	if query.Get("height") != "" {
		config.OptimalScreenWidth, err = strconv.Atoi(query.Get("height"))
		if err != nil || config.OptimalScreenWidth == 0 {
			logrus.Error("Invalid width")
			config.OptimalScreenWidth = 800
		}
	}
	config.AudioMimetypes = []string{"audio/L16", "rate=44100", "channels=2"}

	logrus.Debug("Connecting to guacd")
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:4444")
	if err != nil {
		logrus.Errorln("error while resolving 127.0.0.1")
		return nil, err
	}

	conn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		logrus.Errorln("error while connecting to guacd", err)
		return nil, err
	}

	stream := guac.NewStream(conn, guac.SocketTimeout)

	logrus.Debug("Connected to guacd")
	if request.URL.Query().Get("uuid") != "" {
		config.ConnectionID = request.URL.Query().Get("uuid")
	}
	logrus.Debugf("Starting handshake with %#v", config)
	err = stream.Handshake(config)
	if err != nil {
		return nil, err
	}
	logrus.Debug("Socket configured")

	return guac.NewStreamInterceptingTunnel(guac.NewSimpleTunnel(stream)), nil
}
