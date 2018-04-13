// smsproc.go
package main

import (
	//"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"wujun/smpp"

	"go.uber.org/zap"
	//"go.uber.org/zap/zapcore"
)

const (
	ESME_AUTHPRICEREQ     uint32 = 0x00000030
	ESME_AUTHPRICECNFMREQ uint32 = 0x00000031
)

const (
	TAG_MT_FLAG         uint16 = 0x0108
	TAG_CPID            uint16 = 0x1004
	TAG_SERVICE_ID      uint16 = 0x0203
	TAG_SENDFLAG        uint16 = 0x0202
	TAG_EX_SIGNATURE    uint16 = 0x0301
	TAG_EX_LONGMSG      uint16 = 0x0302
	TAG_EX_HTTPMSGCOUNT uint16 = 0x0303
)

const (
	AU_ACESSCODE_ERR uint32 = 530 //SP接入码非法
	AU_USER_IS_BLACK uint32 = 701
	// SMGW45-02, 2005-12-21, wzy add end //
	AU_SPID_SERVICEID         uint32 = 706 //企业代码或业务代码非法
	AU_NOT_CANMT_COUNT        uint32 = 708 //没有可下发数量
	AU_SP_NOTFIND             uint32 = 731 //SP不存在
	AU_SPORSERVICE_ISNULL     uint32 = 732 //SP或业务为空
	AU_SERVICE_NOTFIND        uint32 = 733 //业务不存在
	AU_ERR_SV_ONLINESTATUS    uint32 = 734 //非法的业务阶段
	AU_ACKTIMEOUT             uint32 = 735 //消息转发给下级网元时ACK超时
	AU_MSGEXPIRE              uint32 = 736 //消息在等待队列中超过生存期
	AU_IF_BROKEN              uint32 = 737 //接口断连
	AU_ERR_SERVICETYPE        uint32 = 738 //非法的业务类型
	AU_OVER_SERVICERANGE      uint32 = 739 //不在SP服务范围内
	AU_LOW_CREDIT             uint32 = 740 //信用度太低
	AU_SPORSERVICE_OVERTIME   uint32 = 741 //SP或业务的服务期已过期
	AU_NOT_TESTNUM            uint32 = 743 //号码不为测试号码
	AU_CANNOT_PRESENT         uint32 = 744 //业务没有开放赠送
	AU_MSGFORMAT_ERROR        uint32 = 745 //上行消息内容格式错误
	AU_SERVICE_NOTALLOW_ORDER uint32 = 746 //业务不支持的定购方式

	//updated by hyh begin  2011-12-6
	AU_DAY_FLOW_LIMIT   uint32 = 747 //日流量限制
	AU_WEEK_FLOW_LIMIT  uint32 = 748 //周流量限制
	AU_MONTH_FLOW_LIMIT uint32 = 749 //月流量限制
	//modify by gyx 20140626
	AU_YEAR_FLOW_LIMIT uint32 = 750 //年流量限制
	//end
	//end updated by hyh 2011-12-6
	//add by wj
	AU_NO_CONTENTTEMPLATE  uint32 = 760 //内容模板限制
	AU_EXCEED_MAX_SEND_NUM uint32 = 761 //最大下发限制
	AU_URL_CHECK_FAIL      uint32 = 762 //url check error
)

func InitSmppSvr(svr *smpp.Server) {
	if config.WorkSize <= 0 || config.WorkSize > 1000 {
		config.WorkSize = 100
	}
	fmt.Println("threads:", config.WorkSize)
	smpp.InitPool(config.WorkSize, 2*config.WorkSize)
	svr.RegHandleFunc(uint32(smpp.BIND_RECEIVER), OnBind)
	svr.RegHandleFunc(uint32(smpp.BIND_TRANSCEIVER), OnBind)
	svr.RegHandleFunc(uint32(smpp.BIND_TRANSMITTER), OnBind)
	svr.RegHandleFunc(ESME_AUTHPRICEREQ, OnAuthPriceReq)
	svr.RegHandleFunc(ESME_AUTHPRICECNFMREQ, OnAuthPriceCnfm)

}

func OnBind(c *smpp.Connector, msg *smpp.SmppMsg) error {
	rp := &smpp.Smpp_bind_resp{}
	reply := smpp.NewBindResp(rp)
	c.ReplyfromMsg(msg.Header, 0, *reply)
	return nil
}

func OnAuthPriceReq(c *smpp.Connector, msg *smpp.SmppMsg) error {
	//fmt.Println("OnAuthPriceReq 1", *msg)
	var tlvs []smpp.TLV
	sub, _ := smpp.ParseSubmitSM(&msg.Body, &tlvs)

	tvs := smpp.TLVS(tlvs)
	t_mo_flag, _ := tvs.FindTlv(TAG_MT_FLAG)

	if t_mo_flag != nil {
		if t_mo_flag.UInt32() == 0 {
			return onMoAuthPriceReq(c, msg, sub, tvs)
		}
	}

	tlv_cpid, _ := tvs.FindTlv(TAG_CPID)

	tlog := trace.GetLoger(tlv_cpid.String(), sub.Dst_Addr)
	tlog = tlog.With(zap.String("addr", sub.Dst_Addr))
	tlog.Info("Rcv AuthPriceReq", zap.Any("MSGHEAD", msg.Header.Data()), zap.Any("MSGBODY", msg.Body.Data()))
	tlog.Debug("moflag", zap.Any("MOFLAG", t_mo_flag.UInt32()))
	tlog.Debug("detail...", zap.Any("submit", sub), zap.Any("tlvs", tlvs))

	tlv_longmsg, _ := tvs.FindTlv(TAG_EX_LONGMSG)
	var msg_content string
	if tlv_longmsg != nil && len(tlv_longmsg.Value) > 0 {
		var err error
		msg_content, err = smpp.GetUtfStringFromUCS2(tlv_longmsg.Value)
		if err != nil {
			fmt.Println("decode ", err)
		}
	} else {
		msg_content = sub.GetContentString()
	}
	//tlog.Debug("", zap.Any("content", msg_content))

	var out_tlvs smpp.TLVS
	spinfo, services, spruninfo, contents, er := SPS.GetSP(tlv_cpid.String())

	if er != nil {
		//sp no find
		tlog.Warn("Sp no found")
		return replyerror(c, msg.Header, AU_SPID_SERVICEID)
	} else {
		tlog.Debug("Get Sp Ok", zap.Any("spinfo", spinfo), zap.Any("contents", contents))
		//		tmp, e := json.Marshal(contents)
		//		fmt.Println("contents", contents, string(tmp), e)
	}

	if time.Now().After(spinfo.Validate) {
		//超过服务期
		tlog.Warn("sp service time expired")
		return replyerror(c, msg.Header, AU_SPORSERVICE_OVERTIME)
	}

	servicecode, _ := tvs.FindTlv(TAG_SERVICE_ID)

	svr, bok := services.services[servicecode.String()]

	if !bok {
		tlog.Warn("sp service no found", zap.String("servicecode", servicecode.String()))
		return replyerror(c, msg.Header, AU_SERVICE_NOTFIND)
	}

	if svr.servicestatus == 0 {
		//forbiden svr
		//fmt.Println("erro service status", svr.servicestatus)
		tlog.Warn("error service status", zap.Any("servicestatus", svr.servicestatus))
		return replyerror(c, msg.Header, AU_SPORSERVICE_OVERTIME)

	}

	now := time.Now()

	span := now.Hour() * 2

	if span < len(svr.timesendcontrol) {

		b := svr.timesendcontrol[span]

		if b > 0 {
			//forbiden send
			tlog.Warn("error send time in service control", zap.Any("timesendcontrol", svr.timesendcontrol))
			return replyerror(c, msg.Header, AU_SPORSERVICE_OVERTIME)
		}
	}

	//增加签名
	if len(svr.Signature) > 0 {
		sig, _ := smpp.GetStringFromUtfString(svr.Signature)
		out_tlvs, _ = out_tlvs.InsertOrReplace(smpp.TLV{Tag: TAG_EX_SIGNATURE, Value: sig})
	}
	tlog.Debug("Add signatue", zap.String("Signature", svr.Signature))

	bok = false
	for _, accessCode := range spinfo.ServiceAccessNumbers {
		if len(accessCode) > 0 && strings.HasPrefix(sub.Src_Addr, accessCode) {
			tlog.Debug("Matched AccessCode", zap.Any("accessCode", accessCode))
			bok = true
			break
		}
	}

	if !bok {
		tlog.Warn("Error AccessCode")
		return replyerror(c, msg.Header, AU_ACESSCODE_ERR)
	}

	bgcheck, g_pFilter := g_filters.filterfun(msg_content)
	tlog.Debug("global filter", zap.Any("filter", g_pFilter), zap.Any("pass", bgcheck))

	if !bgcheck {
		return replyerror(c, msg.Header, AU_NO_CONTENTTEMPLATE)
	}

	tlog.Debug("try check", zap.Any("servicecode", servicecode.String()), zap.Any("addr", sub.Src_Addr), zap.Any("content", msg_content))
	bcheck, pfilter := contents.filterfun(servicecode.String(), sub.Src_Addr, msg_content)

	if !bcheck {
		tlog.Debug("sp conentfilter error", zap.Any("filter", pfilter), zap.Any("content", msg_content))
		return replyerror(c, msg.Header, AU_NO_CONTENTTEMPLATE)

	} else {
		tlog.Debug("sp content filter OK", zap.Any("filter", pfilter), zap.Any("content", msg_content))
	}

	msgcount := 1
	t, er := tvs.FindTlv(TAG_EX_HTTPMSGCOUNT)
	if er == nil {
		msgcount = int(t.UInt32())
	}

	if spinfo.daymaxsendcount > 0 && spruninfo.daysendcount+spruninfo.cachecount+int64(msgcount) > spinfo.daymaxsendcount {
		tlog.Warn("日流量限制")
		return replyerror(c, msg.Header, AU_DAY_FLOW_LIMIT)
	}
	if spinfo.weekmaxsendcount > 0 && spruninfo.weeksendcount+spruninfo.cachecount+int64(msgcount) > spinfo.weekmaxsendcount {
		tlog.Warn("周流量限制")
		return replyerror(c, msg.Header, AU_WEEK_FLOW_LIMIT)

	}
	if spinfo.monmaxsendcount > 0 && spruninfo.monsendcount+spruninfo.cachecount+int64(msgcount) > spinfo.monmaxsendcount {
		tlog.Warn("月流量限制")
		return replyerror(c, msg.Header, AU_MONTH_FLOW_LIMIT)

	}
	if spinfo.yearmaxsendcount > 0 && spruninfo.yearsendcount+spruninfo.cachecount+int64(msgcount) > spinfo.yearmaxsendcount {
		tlog.Warn("年流量限制")
		return replyerror(c, msg.Header, AU_YEAR_FLOW_LIMIT)

	}
	if spruninfo.sendcount+spruninfo.cachecount+int64(msgcount) > spinfo.maxsendcount {
		tlog.Warn("exceed max send count")
		return replyerror(c, msg.Header, AU_EXCEED_MAX_SEND_NUM)
	}

	usercon := DB.GetUserDB(sub.Dst_Addr)
	if usercon.Err() != nil {
		err := fmt.Errorf("user db connect error")
		MngLog.Warn("error:", err)
		return replyerror(c, msg.Header, 759)
	}
	defer usercon.Close()
	//fmt.Println(msgcount, spinfo.Spcode, sub.Dst_Addr, spinfo.userdaymax, 10)
	rt, er := checkUser(int64(msgcount), spinfo.Spcode, sub.Dst_Addr, spinfo.userdaymax, int64(config.UserMaxSnd), usercon)
	if er != nil {
		fmt.Println(er)
	} else {
		if rt == 0 {

		} else if rt == -1 || rt == -2 {
			tlog.Warn("用户流量限制")
			return replyerror(c, msg.Header, AU_NOT_CANMT_COUNT)
		} else {
			tlog.Warn("黑名单")
			return replyerror(c, msg.Header, AU_USER_IS_BLACK)
		}
	}

	AddRuninfo(spruninfo, int64(msgcount))
	out_tlvs, _ = out_tlvs.InsertOrReplace(*smpp.NewTlvUint32(TAG_SENDFLAG, 1))
	rp := smpp.Smpp_submit_sm_resp{}
	rpl := smpp.NewSubmitSMRep(&rp, out_tlvs...)
	c.ReplyfromMsg(msg.Header, 0, *rpl)
	tlog.Debug("Auth OK")
	return nil
}

func onMoAuthPriceReq(c *smpp.Connector, msg *smpp.SmppMsg, sub *smpp.Smpp_submit_sm, tlvs smpp.TLVS) error {
	rp := smpp.Smpp_submit_sm_resp{}
	var out_tlvs smpp.TLVS
	out_tlvs, _ = out_tlvs.InsertOrReplace(*smpp.NewTlvUint32(TAG_SENDFLAG, 1))
	rpl := smpp.NewSubmitSMRep(&rp, out_tlvs...)
	c.ReplyfromMsg(msg.Header, 0, *rpl)
	return nil
}

func OnAuthPriceCnfm(c *smpp.Connector, msg *smpp.SmppMsg) error {

	var tlvs []smpp.TLV
	sub, _ := smpp.ParseSubmitSM(&msg.Body, &tlvs)
	tvs := smpp.TLVS(tlvs)

	tlv_cpid, _ := tvs.FindTlv(TAG_CPID)
	var cpid string
	if tlv_cpid != nil {
		cpid = tlv_cpid.String()
	}

	tlv_mtflag, _ := tvs.FindTlv(TAG_MT_FLAG)
	var mt_flag uint32 = 1
	if tlv_mtflag != nil {
		mt_flag = tlv_mtflag.UInt32()
	}

	var useraddr string
	switch mt_flag {
	case 0:
		useraddr = sub.Src_Addr
	default:
		useraddr = sub.Dst_Addr

	}

	tlog := trace.GetLoger(cpid, useraddr)
	tlog.Info("Rcv AuthCnfm", zap.Any("MSGHEAD", msg.Header.Data()), zap.Any("MSGBODY", msg.Body.Data()))
	tlog.Debug("detail...", zap.Any("submit", sub), zap.Any("tlvs", tlvs))

	rp := smpp.Smpp_submit_sm_resp{}
	var out_tlvs smpp.TLVS
	out_tlvs, _ = out_tlvs.InsertOrReplace(*smpp.NewTlvUint32(TAG_SENDFLAG, 1))
	rpl := smpp.NewSubmitSMRep(&rp, out_tlvs...)
	c.ReplyfromMsg(msg.Header, 0, *rpl)
	return nil
}

func replyerror(c *smpp.Connector, srcheader smpp.SmppHeader, errcode uint32) error {

	rp := smpp.Smpp_submit_sm_resp{}
	rpl := smpp.NewSubmitSMRep(&rp)
	return c.ReplyfromMsg(srcheader, SmppStatus(errcode), *rpl)
}

func AddRuninfo(info *sp_runtime_info, cnt int64) {
	atomic.AddInt64(&info.cachecount, cnt)
}

func SmppStatus(wInCode uint32) uint32 {

	switch wInCode {
	case 513:
		return 22

	case 514:
		return 23

	case 515:

		return 24

	case 519:
		return 25

	case 522:
		return 26

	case 523:
		return 16

	case 524:
		return 27

	case 525:
		return 28

	case 526:
		return 29

	case 528:
		return 30

	case 529:
		return 31
	case 533:
		return 32

	case 534:
		return 33

	case 535:
		return 34

	case 530:
		return 38

		//	if( wInCode == E_ERROR_QUEUE_FULL )
		//		return 39;

		//	if ( wInCode == E_ERROR_LOGINIP)
		//		return 35;

		//	if (wInCode == E_ACCOUNTNAMEERR)
		//		return 14;

		//	if (wInCode == E_ACCOUNTPASSWORDERR)
		//		return 13;

		//	if (wInCode == E_BEYONDMAXIFNUM)
		//		return 36;

		//	if (wInCode == E_ERROR_LOGINOTHER )
		//		return 37;

		//	//***SMGW25-16, 2004-05-25, jdz, add begin***//
		//	if(wInCode == E_ERROR_MSG_FORMAT)
		//	{
		//		return DECODE_NOT_SUPPORT;
		//	}
	//***SMGW25-16, 2004-05-25, jdz, add end***//

	//***SMGW40-01, 2004-12-23, jdz, add begin***//
	default:
		if wInCode >= 700 && wInCode <= 767 {
			//订购关系处理错误码，将内部错误码(700~767)转换为协议中错误码(104~171)
			return wInCode - 596
		}
		//***SMGW40-01, 2004-12-23, jdz, add end***//

		return 255
	}

}
