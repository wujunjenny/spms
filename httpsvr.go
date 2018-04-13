// httpsvr.go
package main

import (
	"strconv"

	"bytes"
	"fmt"
	"sync"

	//"encoding/json"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
	"go.uber.org/zap/zapcore"
)

//var (
//	upgrader = websocket.Upgrader{}
//)

var trace *traceLog = NewTrace()
var MngLog, LogCores = NewLogger()

var log_buff = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func StartHttp(address string) error {

	ec := echo.New()
	ec.Logger.SetLevel(1)
	//group := ec.Group("/", middleware.Logger())

	//group.Static("/", "./web/")
	ec.Static("/", "./static")
	ec.Static("/web", "./web")
	ec.GET("/ManagerTrace", ManagerTrace)
	ec.GET("/SessionTrace", SessionTrace)
	ec.POST("api/contentcheck", ContentCheck)
	ec.GET("api/contentcheck", ContentCheck)
	ec.HideBanner = true
	ec.Use(middleware.Logger())

	return ec.Start(address)
}

func ContentCheck(c echo.Context) error {

	spcode := c.Request().FormValue("spcode")
	service := c.Request().FormValue("service")
	addr := c.Request().FormValue("srcaddr")
	content := c.Request().FormValue("content")

	//buf := new(bytes.Buffer)
	ok, f := g_filters.filterfun(content)

	gresult := struct {
		Result      bool
		Filter      *ContentFilter
		FilterCount int
	}{
		Result:      ok,
		Filter:      f,
		FilterCount: len(g_filters.filters),
	}

	//	json, _ := json.Marshal(gresult)
	//	buf.WriteString(fmt.Sprintln("json:", string(json)))

	//	buf.WriteString(fmt.Sprintln("global filter:", ok))
	//	if f != nil {
	//		buf.WriteString(fmt.Sprintln("pattern:", f.pattern, "option:", f.option))
	//	} else {
	//		buf.WriteString(fmt.Sprintln("pattern:", nil, "option:", nil))

	//	}

	_, _, _, contents, err := SPS.GetSP(spcode)

	spresult := struct {
		Result      bool
		Filter      *ContentFilter
		FilterCount int
		Error       string
	}{}

	if err != nil {
		//buf.WriteString(fmt.Sprintln("sp filter:", false, "error:", err))
		spresult.Error = err.Error()
	} else {
		ok, f := contents.filterfun(service, addr, content)

		spresult.Result = ok
		spresult.Filter = f
		if contents != nil {
			spresult.FilterCount = len(contents.filters)
		}
		//		buf.WriteString(fmt.Sprintln("sp filter:", ok))
		//		buf.WriteString(fmt.Sprintln("filters:", len(contents.filters)))
		//		if f != nil {
		//			buf.WriteString(fmt.Sprintln("pattern:", f.pattern, "option:", f.option))
		//		} else {
		//			buf.WriteString(fmt.Sprintln("pattern:", nil, "option:", nil))

		//		}
	}

	//	buf.WriteString(fmt.Sprintln("rcv param-->",
	//		"spcode:", spcode,
	//		"service:", service,
	//		"srcaddr:", addr,
	//		"content:{{", content, "}}"))
	input := struct {
		Spcode  string `json:"spcode"`
		Service string `json:"service"`
		Srcaddr string `json:"srcaddr"`
		Content string `json:"content"`
	}{
		Spcode:  spcode,
		Service: service,
		Srcaddr: addr,
		Content: content,
	}

	result := struct {
		Global interface{}
		Sp     interface{}
		Input  interface{}
	}{
		Global: gresult,
		Sp:     spresult,
		Input:  input,
	}

	c.JSONPretty(200, result, "\t")
	return nil
}

func ManagerTrace(c echo.Context) error {

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer ws.Close()

	ch := make(chan *bytes.Buffer, 10)
	writer := func(b []byte) (n int, err error) {

		tmp := log_buff.Get().(*bytes.Buffer)
		tmp.Write(b)

		select {
		case ch <- tmp:
			return len(b), nil
		default:
			return 0, fmt.Errorf("Error block write")
		}
		return
	}

	l, _ := strconv.ParseInt(c.QueryParam("level"), 10, 0)
	core := LogCores.AddCoreIO(iowriterFun(writer), zapcore.Level(l))
	MngLog.Debug("start monitor ", c.RealIP())
	defer LogCores.DelCore(core)

	quit := make(chan interface{})

	go func() {
		for {
			select {
			case <-quit:
				return
			default:
			}
			_, _, err := ws.ReadMessage()
			if err != nil {
				close(quit)
				//MngLog.Debug("stop monitor ", c.RealIP())
				return
			}
		}
	}()

	for {
		select {
		case buf := <-ch:
			err = ws.WriteMessage(websocket.TextMessage, buf.Bytes())
			buf.Reset()
			log_buff.Put(buf)
			if err != nil {
				close(quit)
				return err
			}

		case <-quit:
			return err
		}
	}

	return err
}

func SessionTrace(c echo.Context) error {
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)

	if err != nil {
		fmt.Println("Upgrade:", err)
		return err
	}

	defer ws.Close()

	ch := make(chan *bytes.Buffer, 10)
	writer := func(b []byte) (n int, err error) {

		tmp := log_buff.Get().(*bytes.Buffer)
		tmp.Write(b)

		select {
		case ch <- tmp:
			return len(b), nil
		default:
			return 0, fmt.Errorf("Error block write")
		}
		return
	}
	//	writer := func(b []byte) (n int, err error) {
	//		err = ws.WriteMessage(websocket.TextMessage, b)
	//		if err == nil {
	//			n = len(b)
	//		}
	//		return
	//	}
	l, _ := strconv.ParseInt(c.QueryParam("level"), 10, 0)
	patterns := []string{}
	patterns = append(patterns, c.QueryParam("spid"))
	patterns = append(patterns, c.QueryParam("useraddr"))

	MngLog.Debug("start trace ", c.Request().RemoteAddr, "filter:", patterns)
	core := trace.AddTrace(patterns, iowriterFun(writer), int(l))
	defer trace.RemoveTrace(core)
	quit := make(chan interface{})
	go func() {
		for {
			select {
			case <-quit:
				return
			default:
			}
			_, _, err := ws.ReadMessage()
			if err != nil {
				MngLog.Debug("end trace", c.Request().RemoteAddr)
				close(quit)
				return
			}
		}
	}()
	for {
		select {
		case buf := <-ch:
			err = ws.WriteMessage(websocket.TextMessage, buf.Bytes())
			buf.Reset()
			log_buff.Put(buf)
			if err != nil {
				close(quit)
				return err
			}
		case <-quit:
			return err
		}
	}

	return err
}
