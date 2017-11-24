package main

import (
	"github.com/willf/bitset"

	"gopkg.in/mgo.v2/bson"

	"strings"
	"strconv"
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

type BalancerPod struct {
	SwoId
	DepName		string
	WdogAddr	string
	UID		string
	State		int
}

type BalancerRS struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	BalancerId	bson.ObjectId	`bson:"balancerid,omitempty"`
	UID		string		`bson:"uid"`
	WdogAddr	string		`bson:"wdogaddr"`
}

type BalancerLink struct {
	ObjID		bson.ObjectId	`bson:"_id,omitempty"`
	DepName		string		`bson:"depname"`
	Addr		string		`bson:"addr"`
	Port		uint		`bson:"port"`
	NumRS		uint		`bson:"numrs"`
	CntRS		uint		`bson:"cntrs"`
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

func BalancerPodDelAll(depname string) (error) {
	var link *BalancerLink
	var rs []BalancerRS
	var err error

	link = dbBalancerLinkFind(depname)
	if link != nil {
		rs = dbBalancerPodFindAll(link)
		if rs != nil {
			err = dbBalancerPodDelAll(link)
			if err != nil {
				return err
			}

			for _, v := range rs {
				err = balancerDelRS(link, v.WdogAddr)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func BalancerPodDel(depname, uid string) (error) {
	var link *BalancerLink
	var rs *BalancerRS
	var err error

	link = dbBalancerLinkFind(depname)
	if link != nil {
		rs = dbBalancerPodFind(link, uid)
		if rs != nil {
			err = dbBalancerPodDel(link, uid)
			if err != nil {
				return err
			}
			err = balancerDelRS(link, rs.WdogAddr)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func BalancerPodAdd(depname, uid, wdogaddr string) (error) {
	var link *BalancerLink
	var err error

	link = dbBalancerLinkFind(depname)
	if link != nil {
		err = dbBalancerPodAdd(link, uid, wdogaddr)
		if err != nil {
			return err
		}
		err = balancerAddRS(link, wdogaddr)
		if err != nil {
			errdb := dbBalancerPodDel(link, uid)
			if errdb != nil {
				return fmt.Errorf("%s: %s", err.Error(), errdb.Error())
			}
			return err
		}
	} else {
		return fmt.Errorf("No ")
	}

	return nil
}

func BalancerDelete(depname string) (error) {
	var link *BalancerLink
	var err error

	link = dbBalancerLinkFind(depname)
	if link != nil {
		lip := link.lip()

		err = balancerServiceDel(lip)
		if err != nil {
			return err
		}

		putLocalIp <- lip

		err = dbBalancerLinkDel(link)
		if err != nil {
			return err
		}
		err = dbBalancerPodDelAll(link)
		if err != nil {
			return err
		}
	}
	return nil
}

func BalancerCreate(depname string, numrs uint) (error) {
	var err error

	resp := make(chan *LocalIp)
	getLocalIp <- resp
	lip := <-resp
	if lip.Err != nil {
		return lip.Err
	}

	log.Debugf("Allocated %s:%d address, deployment %s", lip.Addr, lip.Port, depname)
	link := &BalancerLink{
		Addr:	 lip.Addr,
		Port:	 lip.Port,
		DepName: depname,
		NumRS:	 numrs,
	}

	err = balancerServiceAdd(lip)
	if err != nil {
		putLocalIp <- lip
		return err
	}

	err = dbBalancerLinkAdd(link)
	if err != nil {
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
		return err
	}

	for _, link := range links {
		setIp(link.lip())
	}
	return nil
}

func notifyPodUpdate(pod *BalancerPod) {
	var err error = nil

	if pod.State == swy.DBPodStateRdy {
		fn, err2 := dbFuncFind(&pod.SwoId)
		if err2 != nil {
			err = err2
			goto out
		}

		logSaveEvent(&fn, "POD", fmt.Sprintf("state: %s", fnStates[fn.State]))
		if fn.State == swy.DBFuncStateBld || fn.State == swy.DBFuncStateUpd {
			err = buildFunction(&fn)
			if err != nil {
				goto out
			}
		} else if fn.State == swy.DBFuncStateBlt || fn.State == swy.DBFuncStateQue {
			dbFuncSetState(&fn, swy.DBFuncStateRdy)
			if fn.OneShot {
				runFunctionOnce(&fn)
			}
		}
	}

	return

out:
	log.Errorf("POD update notify: %s", err.Error())
}

func BalancerInit(conf *YAMLConf) (error) {
	var err error

	localIps = make(map[string]PortRange)
	for _, v := range conf.Balancer.LocalIps {
		log.Debugf("Got %s %s local IPs", v.IP, v.Ports);
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
