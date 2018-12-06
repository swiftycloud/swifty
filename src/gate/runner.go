/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"net/http"
	"fmt"
	"time"
	"context"
	"strings"
	"io/ioutil"

	"swifty/apis"
	"swifty/common"
	"swifty/common/http"
	"swifty/common/xrest"
	"swifty/common/xrest/sysctl"
	"swifty/common/ratelimit"
)

var acceptedContent xh.StringsValues

func init() {
	acceptedContent = xh.MakeStringValues("application/json", "text/plain")

	sysctl.AddSysctl("call_accepted_ctyp",
		func() string { return acceptedContent.String() },
		func (nv string) error {
			acceptedContent = xh.ParseStringValues(nv)
			return nil
		})
}

func makeArgs(args *swyapi.FunctionRun, sopq *statsOpaque, r *http.Request) {
	defer r.Body.Close()

	args.Args = make(map[string]string)

	for k, v := range r.URL.Query() {
		if len(v) < 1 {
			continue
		}

		args.Args[k] = v[0]
		sopq.argsSz += len(k) + len(v[0])
	}

	body, err := ioutil.ReadAll(r.Body)
	if err == nil && len(body) > 0 {
		ct := r.Header.Get("Content-Type")
		ctp := strings.SplitN(ct, ";", 2)
		if len(ctp) > 0 {
			/*
			 * Some comments on the content/type
			 * THe text/plain type is simple
			 * The app/json type means, there's an object
			 * inside and we can decode it rigt in the
			 * runner. On the other hand, decoding the
			 * json into a struct, rather into a generic
			 * map is better for compile-able languages.
			 * Any binary type is better to be handled
			 * with asyncs, as binary data can be big and
			 * tranferring is back and firth is not good.
			 */
			if acceptedContent.Have(ctp[0]) {
				args.ContentType = ctp[0]
				args.Body = string(body)
				sopq.bodySz = len(body)
			}
		}
	}

	args.Method = &r.Method
}

type podConn struct {
	Addr	string
	Host	string
	Port	string
	FnId	string
	PTok	string
}

func talkHTTP(addr, port, url string, args *swyapi.FunctionRun) (*swyapi.WdogFunctionRunResult, error) {
	var resp *http.Response
	var res swyapi.WdogFunctionRunResult
	var err error

	resp, err = xhttp.Req(
			&xhttp.RestReq{
				Address: "http://" + addr + ":" + port + "/v1/run/" + url,
				Timeout: uint(conf.Runtime.Timeout.Max),
			}, args)
	if err != nil {
		if resp == nil {
			wdogErrors.WithLabelValues("NOCODE").Inc()
		} else {
			wdogErrors.WithLabelValues(resp.Status).Inc()
		}
		return nil, err
	}

	err = xhttp.RResp(resp, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func traceTime(sopq *statsOpaque, w string, wt *uint) {
	if sopq != nil && sopq.trace != nil {
		sopq.trace[w] = time.Since(sopq.ts)
		if wt != nil {
			sopq.trace["wdog"] = time.Duration(*wt) * time.Microsecond
		}
	}
}

func (conn *podConn)Run(ctx context.Context, sopq *statsOpaque, suff, event string, args *swyapi.FunctionRun) (*swyapi.WdogFunctionRunResult, error) {
	var res *swyapi.WdogFunctionRunResult
	var err error

	args.Event = event
	proxy := (conf.Wdog.Proxy != 0) && suff == ""

	traceTime(sopq, "wdog.req", nil)

	if proxy {
		res, err = talkHTTP(conn.Host, conf.Wdog.p_port,
				conn.PTok + "/" + strings.Replace(conn.Addr, ".", "_", -1), args)
	} else {
		url := conn.PTok
		if suff != "" {
			url += "/" + suff
		}
		res, err = talkHTTP(conn.Addr, conn.Port, url, args)
	}

	if err != nil {
		return nil, fmt.Errorf("RUN error %s", err.Error())
	}

	traceTime(sopq, "wdog.resp", &res.Time)

	if res.Stdout != "" || res.Stderr != "" {
		go func() {
			sctx, done := mkContext("::logsave")
			defer done(sctx)
			logSaveResult(sctx, conn.FnId, event, res.Stdout, res.Stderr)
		}()
	}

	return res, nil
}

func doRun(ctx context.Context, fn *FunctionDesc, event string, args *swyapi.FunctionRun) (*swyapi.WdogFunctionRunResult, error) {
	fmd, err := memdGetFn(ctx, fn)
	if err != nil {
		ctxlog(ctx).Errorf("Can't %s memdat: %s", fn.Cookie, err.Error())
		return nil, err
	}

	conn, err := balancerGetConnAny(ctx, fmd)
	if conn == nil {
		ctxlog(ctx).Errorf("Can't find %s cookie balancer: %s", fn.Cookie, err.Error())
		return nil, fmt.Errorf("Can't find balancer for %s", fn.Cookie)
	}

	defer balancerPutConn(fmd)
	traceFnEvent(ctx, "run (" + event + ")", fn)

	sopq := statsStart()
	res, err := conn.Run(ctx, sopq, "", event, args)
	if err == nil {
		statsUpdate(fmd, sopq, res, event)
		if sopq.trace != nil {
			traceCall(fmd, args, res, sopq.trace)
		}
	}

	return res, err
}

func doRunBg(ctx context.Context, fn *FunctionDesc, event string, args *swyapi.FunctionRun) {
	_, err := doRun(ctx, fn, event, args)
	if err != nil {
		ctxlog(ctx).Errorf("bg.%s: error running fn: %s", event, err.Error())
	}
}

func prepareTempRun(ctx context.Context, fn *FunctionDesc, td *TenantMemData, params *swyapi.FunctionSources, w http.ResponseWriter) (string, *xrest.ReqErr) {
	if td.runrate == nil {
		td.runrate = xrl.MakeRL(0, uint(conf.RunRate))
	}

	if !td.runrate.Get() {
		http.Error(w, "Try-run is once per second", http.StatusTooManyRequests)
		return "", nil
	}

	suff, err := putTempSources(ctx, fn, params)
	if err != nil {
		return "", GateErrE(swyapi.GateGenErr, err)
	}

	err = tryBuildFunction(ctx, fn, suff)
	if err != nil {
		return "", GateErrM(swyapi.GateGenErr, "Error building function")
	}

	return suff, nil
}
