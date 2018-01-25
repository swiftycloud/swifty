package main

import (
	"github.com/willf/bitset"

	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2"

	"time"
	"net"
	"strings"
	"strconv"
	"context"
	"fmt"

	"../common"
)

var balancerMaxIPCount uint = 255
var balancerIPCount uint = 0

type PortRange struct {
	From	uint
	To	uint
	Busy	*bitset.BitSet
}

type LocalIp struct {
	Addr	string
	Port	uint
	Err	error
}

func makePortRange(ports string) (PortRange, error) {
	ret := PortRange{}

	pp := strings.Split(ports, ":")
	if len(pp) != 2 {
		return ret, fmt.Errorf("balancer: Bad port range in config")
	}

	p, _ := strconv.Atoi(pp[0])
	ret.From = uint(p)
	p, _ = strconv.Atoi(pp[1])
	ret.To = uint(p)

	if (ret.To < ret.From) {
		return ret, fmt.Errorf("balancer: Negative port range")
	}

	ret.Busy = bitset.New(ret.To - ret.From + 1)
	return ret, nil
}

var localIps map[string]PortRange
var getLocalIp chan chan *LocalIp
var putLocalIp chan *LocalIp

func getIp() (*LocalIp) {
	if balancerIPCount < balancerMaxIPCount {
		for ip, ports := range localIps {
			bit, ok := ports.Busy.NextClear(0)
			if !ok {
				continue
			}

			ports.Busy.Set(bit)
			balancerIPCount += 1

			return &LocalIp{ Addr: ip, Port: ports.From + bit }
		}
	}

	return &LocalIp{ Err: fmt.Errorf("balancer: No space left to allocate virtual IP") }
}

func setIp(lip *LocalIp) {
	ports := localIps[lip.Addr]
	ports.Busy.Set(lip.Port - ports.From)
	balancerIPCount += 1
}

func putIp(lip *LocalIp) {
	ports := localIps[lip.Addr]
	ports.Busy.Clear(lip.Port - ports.From)
	balancerIPCount -= 1
}

func manageLocalIps() {
	for {
		select {
		case getr := <-getLocalIp: getr <- getIp()
		case putr := <-putLocalIp: putIp(putr)
		}
	}
}

type BalancerRS struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	BalancerId	bson.ObjectId	`bson:"balancerid,omitempty"`
	UID		string		`bson:"uid"`
	WdogAddr	string		`bson:"wdogaddr"`
	FnId		string		`bson:"fnid"`
	Version		string		`bson:"fnversion"`
}

func (rs *BalancerRS)VIP() string {
	return rs.WdogAddr
}

type BalancerLink struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	FnId		string		`bson:"fnid"`
	DepName		string		`bson:"depname"`
	Addr		string		`bson:"addr"`
	Port		uint		`bson:"port"`
	NumRS		uint		`bson:"numrs"`
	CntRS		uint		`bson:"cntrs"`
	Public		bool		`bson:"public"`
}

func (link *BalancerLink) VIP() string {
	return link.Addr + ":" + strconv.Itoa(int(link.Port))
}

func (link *BalancerLink) lip() (*LocalIp) {
	return &LocalIp{Addr: link.Addr, Port: link.Port}
}

func balancerServiceAdd(lip *LocalIp) (error) {
	args := fmt.Sprintf("-A -t %s:%d -s rr", lip.Addr, lip.Port)
	_, stderr, err := swy.Exec("ipvsadm", strings.Split(args, " "))
	if stderr.String() != "" {
		return fmt.Errorf("balancer: Can't create service for %s:%d: stderr %s",
				lip.Addr, lip.Port, stderr.String())
	} else if err != nil {
		return fmt.Errorf("balancer: Can't create service for %s:%d: %s",
				lip.Addr, lip.Port, err.Error())
	}
	return nil
}

func balancerServiceDel(lip *LocalIp) (error) {
	args := fmt.Sprintf("-D -t %s:%d", lip.Addr, lip.Port)
	_, stderr, err := swy.Exec("ipvsadm", strings.Split(args, " "))
	if stderr.String() != "" {
		return fmt.Errorf("balancer: Can't delete service for %s:%d: stderr %s",
				lip.Addr, lip.Port, stderr.String())
	} else if err != nil {
		return fmt.Errorf("balancer: Can't delete service for %s:%d: %s",
				lip.Addr, lip.Port, err.Error())
	}
	return nil
}

func balancerAddRS(link *BalancerLink, addr string) (error) {
	args := fmt.Sprintf("-a -t %s:%d -m -r %s", link.Addr, link.Port, addr)
	_, stderr, err := swy.Exec("ipvsadm", strings.Split(args, " "))
	if stderr.String() != "" {
		return fmt.Errorf("balancer: Can't create RS for %s:%d/%s: stderr %s",
				link.Addr, link.Port, addr, stderr.String())
	} else if err != nil {
		return fmt.Errorf("balancer: Can't create RS for %s:%d: %s",
				link.Addr, link.Port, err.Error())
	}
	return nil
}

func balancerDelRS(link *BalancerLink, addr string) (error) {
	args := fmt.Sprintf("-d -t %s:%d -r %s", link.Addr, link.Port, addr)
	_, stderr, err := swy.Exec("ipvsadm", strings.Split(args, " "))
	if stderr.String() != "" {
		return fmt.Errorf("balancer: Can't delete RS for %s:%d/%s: stderr %s",
				link.Addr, link.Port, addr, stderr.String())
	} else if err != nil {
		return fmt.Errorf("balancer: Can't delete RS for %s:%d: %s",
				link.Addr, link.Port, err.Error())
	}
	return nil
}

func BalancerPodDel(pod *k8sPod) error {
	var link *BalancerLink
	var rs *BalancerRS
	var err error

	link, err = dbBalancerLinkFindByDepname(pod.DepName)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}

		return fmt.Errorf("No link: %s", err.Error())
	}

	rs, err = dbBalancerPodPop(link, pod)
	if err != nil {
		return fmt.Errorf("Pop error: %s", err.Error())
	}

	err = balancerDelRS(link, rs.WdogAddr)
	if err != nil {
		return fmt.Errorf("IPVS error: %s", err.Error())
	}

	return nil
}

func waitPort(addr_port string) error {
	wt := 100 * time.Millisecond
	var slept time.Duration
	for {
		conn, err := net.Dial("tcp", addr_port)
		if err == nil {
			conn.Close()
			break
		}

		if slept >= SwyPodStartTmo {
			return fmt.Errorf("Pod's port not up for too long")
		}

		/*
		 * Kuber sends us POD-Up event when POD is up, not when
		 * watchdog is ready :) But we need to make sure that the
		 * port is open and ready to serve connetions. Possible
		 * solution might be to make wdog ping us after openeing
		 * its socket, but ... will gate stand that ping flood?
		 *
		 * Moreover, this port waiter is only needed when the fn
		 * is being waited for.
		 */
		glog.Debugf("Port not open yet (%s) ... polling", err.Error())
		<-time.After(wt)
		slept += wt
		wt += 50 * time.Millisecond
	}

	return nil
}

func BalancerPodAdd(pod *k8sPod) error {
	var link *BalancerLink
	var err error

	err = waitPort(pod.WdogAddr)
	if err != nil {
		return err
	}

	link, err = dbBalancerLinkFindByDepname(pod.DepName)
	if err != nil {
		return fmt.Errorf("No link: %s", err.Error())
	}

	err = dbBalancerPodAdd(link, pod)
	if err != nil {
		return fmt.Errorf("Add error: %s", err.Error())
	}

	err = balancerAddRS(link, pod.WdogAddr)
	if err != nil {
		dbBalancerPodDel(link, pod)
		return fmt.Errorf("IPVS error: %s", err.Error())
	}

	fnWaiterKick(link.FnId)
	return nil
}

func BalancerDelete(ctx context.Context, depname string) (error) {
	var link *BalancerLink
	var err error

	link, err = dbBalancerLinkFindByDepname(depname)
	if err != nil {
		if err == mgo.ErrNotFound {
			return nil
		}

		return fmt.Errorf("Link get err: %s", err.Error())
	}

	err = dbBalancerPodDelAll(link)
	if err != nil {
		return fmt.Errorf("POD del all error: %s", err.Error())
	}

	lip := link.lip()
	err = balancerServiceDel(lip)
	if err != nil {
		return err
	}

	putLocalIp <- lip

	err = dbBalancerLinkDel(link)
	if err != nil {
		return fmt.Errorf("Del error: %s", err.Error())
	}

	ctxlog(ctx).Debugf("Removed balancer for %s (ip %s)", depname, lip)

	return nil
}

func BalancerCreate(ctx context.Context, cookie, depname string, numrs uint, public bool) (error) {
	var err error

	resp := make(chan *LocalIp)
	getLocalIp <- resp
	lip := <-resp
	if lip.Err != nil {
		return lip.Err
	}

	ctxlog(ctx).Debugf("Allocated %s:%d address, deployment %s", lip.Addr, lip.Port, depname)
	link := &BalancerLink{
		Addr:	 lip.Addr,
		Port:	 lip.Port,
		DepName: depname,
		FnId:	 cookie,
		NumRS:	 numrs,
		Public:	 public,
	}

	err = balancerServiceAdd(lip)
	if err != nil {
		putLocalIp <- lip
		return err
	}

	err = dbBalancerLinkAdd(link)
	if err != nil {
		ctxlog(ctx).Errorf("balancer-db: Can't insert link %v: %s", link, err.Error())
		errdel := balancerServiceDel(lip)
		if errdel != nil {
			err = fmt.Errorf("%s: %s", err.Error(), errdel.Error())
		}
		putLocalIp <- lip
		return err
	}

	return nil
}

func BalancerLoad(conf *YAMLConf) (error) {
	links, err := dbBalancerLinkFindAll()
	if err != nil {
		glog.Errorf("balancer-db: Can't find links %s/%s: %s", err.Error())
		return err
	}

	for _, link := range links {
		setIp(link.lip())
	}
	return nil
}

func BalancerInit(conf *YAMLConf) (error) {
	var err error

	localIps = make(map[string]PortRange)
	for _, v := range conf.Balancer.LocalIps {
		glog.Debugf("Got %s %s local IPs", v.IP, v.Ports);
		localIps[v.IP], err = makePortRange(v.Ports)
		if err != nil {
			return err
		}
	}

	if BalancerLoad(conf) != nil {
		return fmt.Errorf("balancer: Can't load data from backend")
	}

	getLocalIp = make(chan chan *LocalIp)
	putLocalIp = make(chan *LocalIp)
	go manageLocalIps()

	return nil
}
