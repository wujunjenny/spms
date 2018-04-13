// spms
package main

import "time"
import "sync"
import "github.com/garyburd/redigo/redis"
import "regexp"
import "fmt"
import "encoding/json"
import "strings"
import "strconv"
import "io/ioutil"
import "sync/atomic"

import "os"

//import "go.uber.org/zap"

type SpInfo struct {
	Spcode   string    `json:"spcode"`
	Spname   string    `json:"spname"`
	Validate time.Time `json:"validate"`
	//信誉度
	credit int `json:"credit"`
	//接入码
	ServiceAccessNumbers []string

	maxsendcount int64 `json:"maxsend"`
	//日最大下发数
	daymaxsendcount int64 `json:"maxdaysend"`
	//周最大下发数
	weekmaxsendcount int64 `json:"maxweeksend"`
	//月最大下发数
	monmaxsendcount  int64 `json:"maxmonsend"`
	yearmaxsendcount int64 `json:"maxyearsend"`

	userdaymax int64 `json:"userdaymax"`
	//ServiceContian
	//containts []string
}

type Sp struct {
	info     *SpInfo
	data     *sp_runtime_info
	services *ServiceContian
	Filters  *ContentFilters
	Once
	bloadok bool
}

type SpContian struct {
	sps  map[string]*Sp
	lock *sync.Mutex
}

type sp_runtime_info struct {
	cachecount int64

	sendcount     int64
	daysendcount  int64
	weeksendcount int64
	monsendcount  int64
	yearsendcount int64
}

type ServiceContian struct {
	services map[string]*Serviceinfo
}

type Serviceinfo struct {
	servicecode   string
	servicename   string
	servicedesc   string
	Signature     string
	servicestatus int
	servicetype   int
	feetype       int
	feecode       int
	ServiceCredit int
	//时段控制
	timesendcontrol []int8
}

var SPS SpContian

type dbpool struct {
	spdb             *redis.Pool
	spdb_runtime     *redis.Pool
	dbusers          []*redis.Pool
	script_checkuser *redis.Script
}

func (pool *dbpool) GetSPDB() redis.Conn {
	return pool.spdb.Get()
}

func (pool *dbpool) GetSPRuntimeDB() redis.Conn {
	return pool.spdb_runtime.Get()
}

func (pool *dbpool) GetUserDB(user string) redis.Conn {

	l := len(user)
	if l > 0 {
		end_num := user[l-1]
		end_num = end_num - 0x30
		index := int(end_num) % len(config.UserDBsAddr)
		return pool.dbusers[index].Get()
	}
	return pool.dbusers[0].Get()
}

var DB dbpool

func (sps *SpContian) GetSP(spcode string) (spinfo *SpInfo, spsvrs *ServiceContian, spdata *sp_runtime_info, contents *ContentFilters, err error) {
	sps.lock.Lock()
	sp, ok := sps.sps[spcode]

	if !ok {
		sp = new(Sp)
		sps.sps[spcode] = sp
	}
	sps.lock.Unlock()

	f := func() {
		MngLog.Debug("Load sp ", spcode)
		conn := DB.GetSPDB()
		if conn.Err() != nil {
			conerr := conn.Err()
			err = fmt.Errorf("db connect error:", conerr)
			MngLog.Debug("get config db error:", err, " ", sp.bloadok)
			return
		}
		defer conn.Close()
		info, err := LoadSPInfo(spcode, conn)
		if err != nil {
			MngLog.Warn("loadsp:", info, err)
			return
		}
		sp.info = info

		svr, err := LoadSPServices(spcode, conn)
		if err == nil {
			sp.services = svr
		}

		cs, err := LoadSPContents(spcode, conn)
		sp.Filters = &ContentFilters{filters: cs}
		sp.Filters.MakeFilter2()

		sp.data = &sp_runtime_info{}

		conn1 := DB.GetSPRuntimeDB()
		if conn1.Err() != nil {
			err = fmt.Errorf("runtime db connect error")
			MngLog.Warn("load runtime error:", err)
			return
		}
		defer conn1.Close()
		sp.data.Update(spcode, conn1)
		sp.bloadok = true
	}

	sp.Do(f)
	if sp.bloadok {
		spinfo = sp.info
		spdata = sp.data
		contents = sp.Filters
		spsvrs = sp.services
		err = nil
		//fmt.Println("load contents", sp.Filters, " contents:", contents)
	} else {
		err = fmt.Errorf("load sp %v fail", spcode)
		sp.Reset()
	}
	return
}

func (sps *SpContian) SetSPInfo(spcode string, info *SpInfo) error {
	sps.lock.Lock()
	defer sps.lock.Unlock()
	sp, ok := sps.sps[spcode]
	if ok {
		sp.info = info
		return nil
	} else {
		//		sp = new(Sp)
		//		sps.sps[spcode] = sp
		//		sp.info = info
		//		sp.data = &sp_runtime_info{}
	}

	return nil
}

func (sps *SpContian) SetSPServices(spcode string, svr *ServiceContian) error {
	sps.lock.Lock()
	defer sps.lock.Unlock()
	sp, ok := sps.sps[spcode]
	if ok {
		sp.services = svr
		return nil
	}

	return fmt.Errorf("SP no found")
}

//func (sps *SpContian) SetSPData() error {
//	return
//}

func (sps *SpContian) SetSPFilters(spcode string, cs []ContentFilter) error {
	sps.lock.Lock()
	defer sps.lock.Unlock()
	sp, ok := sps.sps[spcode]
	if ok {
		sp.Filters = &ContentFilters{filters: cs}
		sp.Filters.MakeFilter2()
		MngLog.Debug("build sp.Filters")
		return nil
	}
	return fmt.Errorf("SP no found")
}

const (
	sp_set_info     string = "setinfo"
	sp_load_info    string = "loadinfo"
	sp_set_service  string = "setservice"
	sp_load_service string = "loadservice"
	sp_set_content  string = "setcontents"
	sp_load_content string = "loadcontents"
)

func InitDb() {

	testfun := func(c redis.Conn, t time.Time) error {
		if time.Now().Sub(t).Seconds() > 5.0 {
			_, e := c.Do("PING")
			return e
		}
		return nil
	}

	DB.spdb = redis.NewPool(func() (conn redis.Conn, err error) {
		conn, err = redis.Dial("tcp", config.CfgSvrAddr)
		return
	}, 100)
	DB.spdb.TestOnBorrow = testfun

	DB.spdb_runtime = redis.NewPool(func() (conn redis.Conn, err error) {
		conn, err = redis.Dial("tcp", config.RuntimeSvrAddr)
		return
	}, 100)

	DB.spdb_runtime.TestOnBorrow = testfun

	DB.dbusers = make([]*redis.Pool, len(config.UserDBsAddr))
	f, _ := ioutil.ReadFile("./checkuser.lua")
	DB.script_checkuser = redis.NewScript(2, string(f))

	for i, addr := range config.UserDBsAddr {

		DB.dbusers[i] = redis.NewPool(func() (conn redis.Conn, err error) {
			conn, err = redis.Dial("tcp", addr)
			return
		}, 50)

		DB.dbusers[i].TestOnBorrow = testfun
	}

	go UpdateRuntimeinfo()
}

func InitSPS() {
	SPS.sps = make(map[string]*Sp)
	SPS.lock = &sync.Mutex{}
}

func UpdateRuntimeinfo() {
	for {
		select {
		case <-time.After(time.Second * 10):
			var keys []string
			var sps []*Sp
			SPS.lock.Lock()
			for k, v := range SPS.sps {
				if v.bloadok && v.data.cachecount != 0 {
					keys = append(keys, k)
					sps = append(sps, v)
				}
			}
			SPS.lock.Unlock()

			cn := DB.GetSPRuntimeDB()

			for i := range keys {

				sps[i].data.Update(keys[i], cn)

			}

		}
	}
}

func DbMonitor() {
	conn := DB.GetSPDB()
	defer conn.Close()
	con := redis.PubSubConn{Conn: conn}

	con.Subscribe("msg")
	con.PSubscribe("sp.*")
	LogCores.AddCoreIO(os.Stdout, 0)
	re, er := regexp.Compile(`sp\.(\w+)\.(\w+)`)
	MngLog.Debug(er)

	for {

		switch n := con.Receive().(type) {

		case redis.Message:
			MngLog.Infof("Message: %s %s", n.Channel, n.Data)
		case redis.PMessage:
			MngLog.Infof("PMessage: %s %s %s", n.Pattern, n.Channel, n.Data)
			v := re.FindSubmatch([]byte(n.Channel))
			if v == nil {
				MngLog.Info("unknown channal ", n.Channel, ` validformat like "sp.11111.loadinfo"`)
				continue
			}

			runcmd(string(v[1]), string(v[2]), string(n.Data))

		case redis.Subscription:
			MngLog.Infof("Subscription: %s %s %d", n.Kind, n.Channel, n.Count)
			if n.Count == 0 {
				return
			}
		case error:
			MngLog.Errorf("error: %v", n)
			return

		}

	}

}

func runcmd(spcode string, cmd string, data string) {
	conn := DB.GetSPDB()
	defer conn.Close()

	switch cmd {
	case sp_set_info:

		f, err := ioutil.ReadFile("./setspinfo.lua")

		rt, err := conn.Do("Eval", f, 2, spcode, data)

		MngLog.Info(redis.String(rt, err))

		info, err := LoadSPInfo(spcode, conn)

		if err != nil {
			MngLog.Warnf("Load SP info err %v", err)
			return
		}

		if err = SPS.SetSPInfo(spcode, info); err != nil {
			return
		}

		MngLog.Info("Set SP info ok", info)

	case sp_set_service:

		f, err := ioutil.ReadFile("./setservice.lua")
		rt, err := conn.Do("Eval", f, 2, spcode, data)
		MngLog.Info(redis.String(rt, err))
		services, err := LoadSPServices(spcode, conn)

		if err != nil {
			MngLog.Warnf("Load SP services err %v", err)
			return
		}

		if err = SPS.SetSPServices(spcode, services); err != nil {
			return
		}

		MngLog.Info("Set SP services ok")

	case sp_set_content:

		f, err := ioutil.ReadFile("./setcontents.lua")

		rt, err := conn.Do("Eval", f, 2, spcode, data)

		MngLog.Info(redis.StringMap(rt, err))

		filters, err := LoadSPContents(spcode, conn)

		if err != nil {
			MngLog.Warnf("Load Filters err %v", err)
			return
		}

		if spcode == "global" {
			g_filters.filters = filters
			g_filters.MakeFilter1()
			MngLog.Info("Set Global Filter OK")
			return
		}

		if err = SPS.SetSPFilters(spcode, filters); err != nil {
			MngLog.Info("Set Sp Filter Fail", err)
			return
		}

		MngLog.Info("Set SP Filters ok")

	case sp_load_info:
		info, err := LoadSPInfo(spcode, conn)
		j, _ := json.Marshal(info)
		MngLog.Info(string(j), err)
		if err = SPS.SetSPInfo(spcode, info); err != nil {
			return
		}
		MngLog.Info("Set SP info ok")

		//test
		//		_, _, data, _, _ := SPS.GetSP(spcode)
		//		data.cachecount++
		//		data.Update(spcode, conn)
		//		monitorlog.Info(data)

	case sp_load_service:
		rt, err := LoadSPServices(spcode, conn)
		j, _ := json.Marshal(rt.services)
		MngLog.Info(string(j), err)
		if err = SPS.SetSPServices(spcode, rt); err != nil {
			return
		}
		MngLog.Info("Set SP services ok")

	case sp_load_content:
		rt, err := LoadSPContents(spcode, conn)
		j, _ := json.Marshal(rt)
		MngLog.Info(string(j), err)

		if spcode == "global" {
			g_filters.filters = rt
			g_filters.MakeFilter1()
			MngLog.Info("Load Global Filter OK")
			return
		}

		if err = SPS.SetSPFilters(spcode, rt); err != nil {
			return
		}
		MngLog.Info("Set SP Filters ok")

	default:
		MngLog.Errorf("unkown cmd [%v]", cmd)
	}

}

func (spinfo *SpInfo) MarshalJSON() ([]byte, error) {
	tmp := &struct {
		Spcode   string `json:"spcode"`
		Spname   string `json:"spname"`
		Validate string `json:"validate"`
		//信誉度
		Credit int `redis:"credit" json:"credit"`
		//接入码
		ServiceAccessNumbers []string `json:"accessnumbers"`

		Maxsendcount int64 `redis:"maxsend" json:"maxsend"`
		//日最大下发数
		Daymaxsendcount int64 `redis:"maxdaysend" json:"maxdaysend"`
		//周最大下发数
		Weekmaxsendcount int64 `redis:"maxweeksend" json:"maxweeksend"`
		//月最大下发数
		Monmaxsendcount  int64 `redis:"maxmonsend" json:"maxmonsend"`
		Yearmaxsendcount int64 `json:"maxyearsend"`
		UserDayMax       int64 `json:"userdaymax"`
	}{

		Spcode:   spinfo.Spcode,
		Spname:   spinfo.Spname,
		Validate: spinfo.Validate.Format("2006-01-02"),
		//信誉度
		Credit: spinfo.credit,
		//接入码
		ServiceAccessNumbers: spinfo.ServiceAccessNumbers,

		Maxsendcount: spinfo.maxsendcount,
		//日最大下发数
		Daymaxsendcount: spinfo.daymaxsendcount,
		//周最大下发数
		Weekmaxsendcount: spinfo.weekmaxsendcount,
		//月最大下发数
		Monmaxsendcount:  spinfo.maxsendcount,
		Yearmaxsendcount: spinfo.yearmaxsendcount,
		UserDayMax:       spinfo.userdaymax,
	}

	return json.Marshal(tmp)

}

func (spinfo *SpInfo) UnmarshalJSON(b []byte) error {

	tmp := &struct {
		Spcode   string `json:"spcode"`
		Spname   string `json:"spname"`
		Validate string `json:"validate"`
		//信誉度
		Credit int `redis:"credit" json:"credit"`
		//接入码
		ServiceAccessNumbers []string `json:"accessnumbers"`

		Maxsendcount int64 `json:"maxsend"`
		//日最大下发数
		Daymaxsendcount int64 `json:"maxdaysend"`
		//周最大下发数
		Weekmaxsendcount int64 `json:"maxweeksend"`
		//月最大下发数
		Monmaxsendcount  int64 `json:"maxmonsend"`
		Yearmaxsendcount int64 `json:"maxyearsend"`
		UserDayMax       int64 `json:userdaymax`
	}{}

	err := json.Unmarshal(b, tmp)

	spinfo.Spcode = tmp.Spcode
	spinfo.Spname = tmp.Spname
	spinfo.credit = tmp.Credit
	spinfo.ServiceAccessNumbers = tmp.ServiceAccessNumbers
	spinfo.Validate, _ = time.Parse("2006-01-02", tmp.Validate)
	spinfo.maxsendcount = tmp.Maxsendcount
	spinfo.daymaxsendcount = tmp.Daymaxsendcount
	spinfo.weekmaxsendcount = tmp.Weekmaxsendcount
	spinfo.monmaxsendcount = tmp.Monmaxsendcount
	spinfo.yearmaxsendcount = tmp.Yearmaxsendcount
	spinfo.userdaymax = tmp.UserDayMax
	return err
}

func (service *Serviceinfo) MarshalJSON() ([]byte, error) {
	temp := &struct {
		Servicecode     string `json:"servicecode"`
		Servicename     string `json:"servicename"`
		Servicedesc     string `json:"servicedesc"`
		Signature       string `json:"signature"`
		Servicestatus   int    `json:"status"`
		Servicetype     int    `json:"type"`
		Feetype         int    `json:"feetype"`
		Feecode         int    `json:"feecode"`
		ServiceCredit   int    `json:"credit"`
		Timesendcontrol string `json:"timecontrol"`
	}{
		Servicecode:   service.servicecode,
		Servicename:   service.servicename,
		Servicedesc:   service.servicedesc,
		Signature:     service.Signature,
		Servicestatus: service.servicestatus,
		Servicetype:   service.servicetype,
		Feetype:       service.feetype,
		Feecode:       service.feecode,
		ServiceCredit: service.ServiceCredit,
	}

	for _, v := range service.timesendcontrol {
		temp.Timesendcontrol = temp.Timesendcontrol + strconv.FormatInt(int64(v), 8) + ":"
	}

	return json.Marshal(temp)
}

func (sevice *Serviceinfo) UnmarshalJSON(b []byte) error {
	tmp := &struct {
		Servicecode   string `json:"servicecode"`
		Servicename   string `json:"servicename"`
		Servicedesc   string `json:"servicedesc"`
		Signature     string `json:"signature"`
		Servicestatus int    `json:"status"`
		Servicetype   int    `json:"type"`
		Feetype       int    `json:"feetype"`
		Feecode       int    `json:"feecode"`
		ServiceCredit int    `json:"credit"`
		//时段控制
		Timesendcontrol string `json:"timecontrol"`
	}{}
	err := json.Unmarshal(b, tmp)

	sevice.servicecode = tmp.Servicecode
	sevice.servicename = tmp.Servicename
	sevice.servicedesc = tmp.Servicedesc
	sevice.servicestatus = tmp.Servicestatus
	sevice.servicetype = tmp.Servicetype
	sevice.Signature = tmp.Signature

	spans := strings.Split(tmp.Timesendcontrol, ":")

	sevice.timesendcontrol = make([]int8, len(spans))
	for i, v := range spans {

		intv, _ := strconv.ParseInt(v, 10, 8)
		sevice.timesendcontrol[i] = int8(intv)

	}

	return err
}

func LoadSPInfo(spcode string, conn redis.Conn) (spinfo *SpInfo, err error) {

	spinfo = &SpInfo{}
	rply, err := conn.Do("HMGET", "sp."+spcode,
		"spcode",
		"spname",
		"validate",
		"credit",
		"maxsend",
		"maxdaysend",
		"maxweeksend",
		"maxmonsend",
		"maxyearsend",
		"accessnumbers",
		"userdaymax",
	)

	if err != nil {

		return nil, err
	}
	//fmt.Println(spcode, len(spcode), rply)
	if rply == nil {
		return nil, fmt.Errorf("error sp no found")
	}

	vs, err := redis.Values(rply, err)
	if err != nil {
		MngLog.Info(err)
		return nil, err
	}

	var gerr error

	if vs[0] != nil {
		spinfo.Spcode, gerr = redis.String(vs[0], err)
		if gerr != nil {
			return nil, gerr
		}
	} else {
		return nil, fmt.Errorf("Sp no found :%v", spcode)
	}

	if vs[1] != nil {
		spinfo.Spname, gerr = redis.String(vs[1], err)
		if gerr != nil {
			return nil, gerr
		}
	}

	var stemp string

	if vs[2] != nil {
		stemp, gerr = redis.String(vs[2], err)
		if gerr != nil {
			return nil, gerr
		}
		spinfo.Validate, gerr = time.Parse("2006-01-02", stemp)
	}

	MngLog.Info("step 1")

	if vs[3] != nil {
		spinfo.credit, gerr = redis.Int(vs[3], err)
		if gerr != nil {
			MngLog.Error("credit get error ", gerr)
		}
	}
	spinfo.maxsendcount, gerr = redis.Int64(vs[4], err)
	if gerr != nil {
		MngLog.Error("maxsendcount get error ", gerr)
	}

	spinfo.daymaxsendcount, gerr = redis.Int64(vs[5], err)
	if gerr != nil {
		MngLog.Error("daymaxsendcount get error ", gerr)
	}
	spinfo.weekmaxsendcount, gerr = redis.Int64(vs[6], err)
	if gerr != nil {
		MngLog.Error("weekmaxsendcount get error ", gerr)
	}
	spinfo.monmaxsendcount, gerr = redis.Int64(vs[7], err)
	if gerr != nil {
		MngLog.Error("monmaxsendcount get error ", gerr)
	}
	spinfo.yearmaxsendcount, gerr = redis.Int64(vs[8], err)
	if gerr != nil {
		MngLog.Error("yearmaxsendcount get error ", gerr)
	}

	if vs[9] != nil {
		stemp, gerr = redis.String(vs[9], err)
		if gerr != nil {

			return nil, gerr
		}

		spinfo.ServiceAccessNumbers = strings.Split(stemp, "|")
	}

	if vs[10] != nil {
		spinfo.userdaymax, gerr = redis.Int64(vs[10], err)
		if gerr != nil {
			MngLog.Error("userdaymax get error ", gerr)
		}
	}

	return
}

func LoadSPServices(spcode string, conn redis.Conn) (srt *ServiceContian, err error) {

	lsvr := &ServiceContian{}
	lsvr.services = make(map[string]*Serviceinfo)
	ssetkey := "services:" + spcode
	rply, err := conn.Do("SMEMBERS", ssetkey)

	sset, err := redis.Strings(rply, err)

	if err != nil {
		MngLog.Warn("Get servcie set err ", err)
		return nil, err
	}
	MngLog.Infof("count[%v],%v ", len(sset), sset)

	for _, v := range sset {

		service, err := LoadSPService(spcode, v, conn)
		if err != nil {
			return nil, fmt.Errorf("load service error spcode[%v] servicecode[%v] err[%v]", spcode, v, err)
		}
		lsvr.services[v] = service
		t, _ := json.Marshal(service)
		MngLog.Info(string(t))
	}
	srt = lsvr
	return
}

func LoadSPService(spcode string, servicecode string, conn redis.Conn) (svr *Serviceinfo, err error) {

	service := &Serviceinfo{}
	key := "service:" + spcode + ":" + servicecode
	rply, err := conn.Do("HMGET", key, "servicecode",
		"servicename",
		"servicedesc",
		"signature",
		"status",
		"type",
		"feetype",
		"feecode",
		"credit",
		"timecontrol")
	if err != nil {
		MngLog.Warnf("HMGET error[%v] key[%v]", err, key)
		return nil, err
	}

	vs, err := redis.Values(rply, err)

	if vs[0] != nil {
		service.servicecode, err = redis.String(vs[0], err)
		if err != nil {
			return
		}
	}

	if vs[1] != nil {
		service.servicename, err = redis.String(vs[1], err)
		if err != nil {
			return
		}
	}
	if vs[2] != nil {
		service.servicedesc, err = redis.String(vs[2], err)
		if err != nil {
			return
		}
	}
	if vs[3] != nil {
		service.Signature, err = redis.String(vs[3], err)
		if err != nil {
			return
		}
	}
	if vs[4] != nil {
		service.servicestatus, err = redis.Int(vs[4], err)
		if err != nil {
			return
		}
	}
	if vs[5] != nil {
		service.servicetype, err = redis.Int(vs[5], err)
		if err != nil {
			return
		}
	}
	if vs[6] != nil {
		service.feetype, err = redis.Int(vs[6], err)
		if err != nil {
			return
		}
	}
	if vs[7] != nil {
		service.feecode, err = redis.Int(vs[7], err)
		if err != nil {
			return
		}
	}
	if vs[8] != nil {
		service.ServiceCredit, err = redis.Int(vs[8], err)
		if err != nil {
			return
		}
	}
	if vs[9] != nil {
		stime, err := redis.String(vs[9], err)
		if err != nil {
			return nil, err
		}
		times := strings.Split(stime, ":")

		for _, v := range times {

			var i int8
			if len(v) == 0 {
				i = 0
			} else {
				temp, _ := strconv.ParseInt(v, 10, 8)
				i = int8(temp)
			}

			service.timesendcontrol = append(service.timesendcontrol, i)
		}

	}
	svr = service
	return
}

func LoadSPContents(spcode string, conn redis.Conn) (contents []ContentFilter, err error) {

	setkey := "contents:" + spcode
	rply, err := conn.Do("SMEMBERS", setkey)
	cs, err := redis.Strings(rply, err)
	if err != nil {
		return nil, err
	}

	var rt []ContentFilter

	for _, v := range cs {

		var c ContentFilter
		if err = json.Unmarshal([]byte(v), &c); err == nil {
			rt = append(rt, c)
		} else {
			MngLog.Warnf("eror content format ", v)
		}
	}
	return rt, nil
}

func (info *sp_runtime_info) Update(spcode string, conn redis.Conn) error {
	old := atomic.SwapInt64(&info.cachecount, 0)

	now := time.Now()
	conn.Send("MULTI")
	key := "sp:" + spcode + ".sendcount"
	conn.Send("INCRBY", key, old)

	key = "sp:" + spcode + ".daysendcount"
	conn.Send("INCRBY", key, old)
	conn.Send("EXPIREAT", key, nextDay(now).Unix())

	key = "sp:" + spcode + ".weeksendcount"
	conn.Send("INCRBY", key, old)
	conn.Send("EXPIREAT", key, nextWeek(now).Unix())

	key = "sp:" + spcode + ".monsendcount"
	conn.Send("INCRBY", key, old)
	conn.Send("EXPIREAT", key, nextMonth(now).Unix())

	key = "sp:" + spcode + ".yearsendcount"
	conn.Send("INCRBY", key, old)
	conn.Send("EXPIREAT", key, nexYear(now).Unix())

	r, err := redis.Values(conn.Do("EXEC"))
	if err != nil {
		atomic.AddInt64(&info.cachecount, old)
		MngLog.Warn("Update Runtime Date Error ", spcode, err)
		return err
	}

	info.sendcount, _ = redis.Int64(r[0], err)
	info.daysendcount, _ = redis.Int64(r[1], err)
	info.weeksendcount, _ = redis.Int64(r[3], err)
	info.monsendcount, _ = redis.Int64(r[5], err)
	info.yearsendcount, _ = redis.Int64(r[7], err)
	MngLog.Debug("update Runtime OK ", spcode, old)
	return nil
}

func checkUser(cnt int64, spcode string, usr string, sp_daymax int64, daymax int64, conn redis.Conn) (rt int, err error) {

	now := time.Now()
	span := nextDay(now).Sub(now)
	result, err := DB.script_checkuser.Do(
		conn,
		//keys
		usr, spcode,
		//args
		cnt, sp_daymax, daymax, int(span.Seconds()))

	if err != nil {
		return -1, err
	}

	rt, err = redis.Int(result, err)

	return
}

func nextDay(t time.Time) time.Time {
	y, m, d := t.Date()
	newday := time.Date(y, m, d+1, 0, 0, 0, 0, time.Local)
	return newday
}

func nextWeek(t time.Time) time.Time {
	y, m, d := t.Date()
	week := t.Weekday()
	newdate := time.Date(y, m, d+7-int(week), 0, 0, 0, 0, time.Local)
	return newdate
}

func nextMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	newMon := time.Date(y, m+1, 1, 0, 0, 0, 0, time.Local)
	return newMon
}

func nexYear(t time.Time) time.Time {
	y, _, _ := t.Date()
	newDate := time.Date(y+1, 1, 1, 0, 0, 0, 0, time.Local)
	return newDate
}
