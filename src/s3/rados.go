package main

import (
	// dnf -y install gcc librados2-devel librbd-devel
	"github.com/ceph/go-ceph/rados"

	"encoding/json"
	"fmt"
)

var radosConn *rados.Conn
var radosDisabled bool

const (
	CephPoolQuoteMaxObjects	= "max_objects"
	CephPoolQuoteMaxBytes	= "max_bytes"
)

type CephMonCmdSetPoolQuota struct {
	Prefix		string		`json:"prefix"`
	Pool		string		`json:"pool"`
	Field		string		`json:"field"`
	Value		string		`json:"val"`
}

func radosSetQuotaOnField(pool, field, value string) error {
	var req []byte
	var err error

	if radosDisabled {
		return nil
	}

	req, err = json.Marshal(&CephMonCmdSetPoolQuota{
				Prefix:	"osd pool set-quota",
				Pool:	pool,
				Field:	field,
				Value:	value,
			})
	if err != nil {
		log.Errorf("rados: Can't marshal request on %s/%s/%s",
				pool, field, value)
		return err
	}

	_, _, err = radosConn.MonCommand(req)
	if err != nil {
		log.Errorf("rados: Can't execute request on %s/%s/%s",
				pool, field, value)
		return err
	}

	return nil
}

func radosSetQuota(pool string, max_objects, max_bytes uint64) error {
	var err error

	if radosDisabled {
		return nil
	}

	err = radosSetQuotaOnField(pool,
			CephPoolQuoteMaxObjects,
			fmt.Sprintf("%d", max_objects))
	if err == nil {
		err = radosSetQuotaOnField(pool,
			CephPoolQuoteMaxBytes,
			fmt.Sprintf("%d", max_bytes))
	}

	return err
}

func radosDeletePool(pool string) error {
	var err error

	if radosDisabled {
		return nil
	}

	err = radosConn.DeletePool(pool)
	if err != nil {
		log.Errorf("rados: Can't delete pool %s: %s", pool, err.Error())
		return err
	}

	log.Debugf("rados: Pool %s is deleted", pool)
	return nil
}

func radosCreatePool(pool string, max_objects, max_bytes uint64) error {
	var err error

	if radosDisabled {
		return nil
	}

	log.Debugf("rados: Creating pool %s", pool)

	err = radosConn.MakePool(pool)
	if err != nil {
		log.Errorf("rados: Can't create pool %s: %s", pool, err.Error())
		return err
	}

	if max_objects != 0 || max_bytes != 0 {
		err = radosSetQuota(pool, max_objects, max_bytes)
	}

	if err == nil {
		log.Debugf("rados: Pool %s is created (quota objects %d bytes %d)",
				pool, max_objects, max_bytes)
	} else {
		radosDeletePool(pool)
	}

	return err
}

func radosWriteObject(pool, oname string, data []byte) error {
	var ioctx *rados.IOContext
	var err error

	if radosDisabled {
		return nil
	}

	ioctx, err = radosConn.OpenIOContext(pool)
	if err != nil {
		log.Errorf("rados: Can't open context for pool %s object %s: %s",
				pool, oname, err.Error())
	}

	err = ioctx.Write(oname, data, 0)
	if err != nil {
		log.Errorf("rados: Can't write object for pool %s object %s: %s",
				pool, oname, err.Error())
		ioctx.Destroy()
		return err
	}

	log.Debugf("rados: Wrote pool %s object %s size %d",
			pool, oname, len(data))

	ioctx.Destroy()
	return nil
}

// FIXME: We can read up to int value at once
func radosReadObject(pool, oname string, size uint64) ([]byte, error) {
	var ioctx *rados.IOContext
	var data []byte
	var err error
	var n int

	if radosDisabled {
		return nil, nil
	}

	ioctx, err = radosConn.OpenIOContext(pool)
	if err != nil {
		log.Errorf("rados: Can't open context for pool %s object %s: %s",
				pool, oname, err.Error())
		return nil, err
	}

	data = make([]byte, size)
	n, err = ioctx.Read(oname, data, 0)
	if err != nil {
		log.Errorf("rados: Can't read object from pool %s object %s: %s",
				pool, oname, err.Error())
		ioctx.Destroy()
		return nil, err
	}

	log.Debugf("rados: Read pool %s object %s size %d",
			pool, oname, n)

	ioctx.Destroy()
	return data, nil
}

func radosDeleteObject(pool, oname string) error {
	var ioctx *rados.IOContext
	var err error

	if radosDisabled {
		return nil
	}

	ioctx, err = radosConn.OpenIOContext(pool)
	if err != nil {
		log.Errorf("rados: Can't open context for pool %s object %s: %s",
				pool, oname, err.Error())
	}

	err = ioctx.Delete(oname)
	if err != nil {
		log.Errorf("rados: Can't delete object for pool %s object %s: %s",
				pool, oname, err.Error())
		ioctx.Destroy()
		return err
	}

	log.Debugf("rados: Delete pool %s object %s",
			pool, oname)

	ioctx.Destroy()
	return nil
}

func radosInit(conf *YAMLConf) error {
	var err error

	if radosDisabled {
		return nil
	}

	radosConn,_ = rados.NewConn()
	err = radosConn.ReadConfigFile(conf.Ceph.ConfigPath)
	if err != nil {
		log.Fatalf("Can't read config of Ceph: %s",
		err.Error())
	}

	err = radosConn.Connect()
	if err != nil {
		log.Fatalf("Can't setup connection to Ceph: %s",
		err.Error())
	}

	return nil
}

func radosFini() {
	if radosConn != nil {
		radosConn.Shutdown()
		radosConn = nil
	}
}
