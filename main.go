// main.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"

	"html/template"
	"wujun/smpp"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
	"github.com/julienschmidt/httprouter"
	"github.com/mitchellh/panicwrap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:    4096,
	WriteBufferSize:   4096,
	EnableCompression: true,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var textfmt = &log.TextFormatter{
	TimestampFormat: "2006-01-02 15-04-05.9999",
	DisableColors:   true}

var jsonfmt = &log.JSONFormatter{
	TimestampFormat: "2006-01-02 15-04-05.9999",
	FieldMap: log.FieldMap{
		log.FieldKeyTime:  "T",
		log.FieldKeyLevel: "L",
		log.FieldKeyMsg:   "M",
	},
}

var monitorhook = MonitorHook{
	formatter: jsonfmt,
	conns:     make(map[*websocket.Conn]chan []byte),
}

type GConfig struct {
	HTTPAddr       string   `json:"http"`
	SmppAddr       string   `json:"smpp"`
	SmppUser       string   `json:"smpp-user"`
	SmppPassword   string   `json:"smpp-password"`
	WorkSize       int      `json:"work-threads"`
	CfgSvrAddr     string   `json:"config-sever"`
	RuntimeSvrAddr string   `json:"runtimeSvrAddr"`
	UserDBsAddr    []string `json:"users-db"`
	UserMaxSnd     int64    `json:"user-max-snd"`
}

var config GConfig

var monitorlog1 = log.New()

func panicHandler(output string) {
	filename := time.Now().Format("panic2006-01-02-15-04-05.log")
	f, _ := os.Create(filename)
	f.WriteString(output)
	f.Close()
}

func main() {

	exitStatus, waperr := panicwrap.BasicWrap(panicHandler)
	if waperr != nil {
		// Something went wrong setting up the panic wrapper. Unlikely,
		// but possible.
		panic(waperr)
	}

	// If exitStatus >= 0, then we're the parent process and the panicwrap
	// re-executed ourselves and completed. Just exit with the proper status.
	if exitStatus >= 0 {
		os.Exit(exitStatus)
	}

	f, _ := os.OpenFile("mo.log", os.O_WRONLY|os.O_CREATE, 0755)
	log.SetOutput(f)
	file, e := ioutil.ReadFile("./config.json")
	if e != nil {
		fmt.Printf("File error: %v\n", e)
		return
	}

	//monitorlog.Hooks.Add(&monitorhook)
	//fmt.Printf("%s\n", string(file))

	//m := new(Dispatch)
	//var m interface{}
	//var config GConfig
	err := json.Unmarshal(file, &config)
	if err != nil {
		log.Errorf("Error read config %v", err)
		return
	}
	showconfig()

	router := httprouter.New()

	router.GET("/ManagerMonitor", ManagerMonitor)

	router.GET("/", home)

	router.HandleMethodNotAllowed = false
	router.ServeFiles("/main/*filepath", http.Dir("./static"))
	router.NotFound = &redirector{}

	InitDb()
	InitSPS()
	InitGlobalFilter()

	smpp_svr := smpp.Server{}
	smpp_svr.Addr = config.SmppAddr

	InitSmppSvr(&smpp_svr)
	go smpp_svr.Run()
	go DbMonitor()

	//	go func() {
	//		for {
	//			select {
	//			case <-time.After(time.Second):
	//				monitorlog.Info("send hello")
	//				//monitorhook.Write([]byte("hello111"))
	//			}
	//		}
	//	}()

	go StartHttp(":9999")
	panic(http.ListenAndServe(config.HTTPAddr, router))

}

func showconfig() {
	//	HTTPAddr       string   `json:"http"`
	//	SmppAddr       string   `json:"smpp"`
	//	SmppUser       string   `json:"smpp-user"`
	//	SmppPassword   string   `json:"smpp-password"`
	//	WorkSize       int      `json:"work-threads"`
	//	CfgSvrAddr     string   `json:"config-sever"`
	//	RuntimeSvrAddr string   `json:"runtimeSvrAddr"`
	//	UserDBsAddr    []string `json:"users-db"`
	//	UserMaxSnd     int      `json:"user-max-snd"`

	fmt.Println("http port:", config.HTTPAddr)
	fmt.Println("Smpp port:", config.SmppAddr)
	fmt.Println("smpp-user:", config.SmppUser)
	fmt.Println("work-threads:", config.WorkSize)
	fmt.Println("user-max-snd:", config.UserMaxSnd)
	fmt.Println("config-server:", config.CfgSvrAddr)
	fmt.Println("users-db:", config.UserDBsAddr)
	fmt.Println("runtimeSvrAddr:", config.RuntimeSvrAddr)

}

func home(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {

	homeTemplate.Execute(w, "ws://"+r.Host+"/ManagerMonitor")
}

type MonitorHook struct {
	formatter log.Formatter
	conns     map[*websocket.Conn]chan []byte
	lock      sync.Mutex
}

func (mo *MonitorHook) Register(conn *websocket.Conn) (rt chan []byte) {
	mo.lock.Lock()
	defer mo.lock.Unlock()

	rt = make(chan []byte, 100)
	mo.conns[conn] = rt

	return
}

func (mo *MonitorHook) Fire(entry *log.Entry) error {

	//fmt.Println("start hook")
	msg, err := mo.formatter.Format(entry)
	if err != nil {
		return err
	}
	//fmt.Println(msg)
	mo.Write(msg)
	return nil
}

func (hook *MonitorHook) Levels() []log.Level {

	return log.AllLevels
}

func (mo *MonitorHook) Write(data []byte) (sz int, err error) {

	mo.lock.Lock()
	defer mo.lock.Unlock()
	for k, v := range mo.conns {

		select {
		case v <- data:
		default:
			//client rcv too slow	remove it
			close(v)
			delete(mo.conns, k)
		}

	}
	err = nil
	sz = len(data)
	return
}

func (mo *MonitorHook) DeRegister(conn *websocket.Conn) {
	mo.lock.Lock()
	defer mo.lock.Unlock()
	ch, ok := mo.conns[conn]
	if ok {
		close(ch)
		delete(mo.conns, conn)
	}
}

func ManagerMonitor(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithField("err", err).Println("Upgrading to websockets")
		http.Error(w, "Error Upgrading to websockets", 400)
		return
	}

	ch := monitorhook.Register(ws)
	log.Info("register  ", ws.RemoteAddr())
	go func() {
		for {
			select {
			case data, ok := <-ch:
				if !ok {
					return
				}
				ws.WriteMessage(websocket.TextMessage, data)

			}
		}
	}()

	for {
		mt, data, err := ws.ReadMessage()
		ctx := log.Fields{"mt": mt, "data": data, "err": err}
		if err != nil {
			if err == io.EOF {
				log.WithFields(ctx).Info("Websocket closed!")
			} else {
				log.WithFields(ctx).Error("Error reading websocket message")
			}
			break
		}

	}

	monitorhook.DeRegister(ws)
	log.Info("deregister  ", ws.RemoteAddr())

}

type redirector struct {
}

func (re *redirector) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	http.Redirect(w, r, "http://"+r.Host+"/main", 301)
}

var homeTemplate = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<head>
<meta charset="utf-8">
<script>  
window.addEventListener("load", function(evt) {
    var output = document.getElementById("output");
    var input = document.getElementById("input");
    var ws;
    var print = function(message) {
        var d = document.createElement("div");
        d.innerHTML = message;
        output.appendChild(d);
    };
    document.getElementById("open").onclick = function(evt) {
        if (ws) {
            return false;
        }
        ws = new WebSocket("{{.}}");
        ws.onopen = function(evt) {
            print("OPEN");
        }
        ws.onclose = function(evt) {
            print("CLOSE");
            ws = null;
        }
        ws.onmessage = function(evt) {
            print("RESPONSE: " + evt.data);
        }
        ws.onerror = function(evt) {
            print("ERROR: " + evt.data);
        }
        return false;
    };
    document.getElementById("send").onclick = function(evt) {
        if (!ws) {
            return false;
        }
        print("SEND: " + input.value);
        ws.send(input.value);
        return false;
    };
    document.getElementById("close").onclick = function(evt) {
        if (!ws) {
            return false;
        }
        ws.close();
        return false;
    };
});
</script>
</head>
<body>
<table>
<tr><td valign="top" width="50%">
<p>Click "Open" to create a connection to the server, 
"Send" to send a message to the server and "Close" to close the connection. 
You can change the message and send multiple times.
<p>
<form>
<button id="open">Open</button>
<button id="close">Close</button>
<p><input id="input" type="text" value="Hello world!">
<button id="send">Send</button>
</form>
</td><td valign="top" width="50%">
<div id="output"></div>
</td></tr></table>
</body>
</html>
`))
