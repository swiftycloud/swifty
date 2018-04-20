package main

import (
	"encoding/json"
	"syscall"
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func readAndOut(f *os.File) {
	var ret string

	buf := make([]byte, 512, 512)
	for {
		n, _ := f.Read(buf)
		if n == 0 {
			fmt.Printf(ret)
			return
		}
		ret += string(buf[:n])
	}
}

func main() {
	skp, err := syscall.Socketpair(syscall.AF_LOCAL, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		fmt.Printf("socketpair error: %s", err.Error())
		return
	}

	sk := os.NewFile(uintptr(skp[1]), "q")
	syscall.CloseOnExec(skp[1])

	oup := make([]int, 2)
	err = syscall.Pipe(oup)
	if err != nil {
		fmt.Printf("pipe error: %s", err.Error())
		return
	}
	syscall.SetNonblock(oup[0], true)
	syscall.CloseOnExec(oup[0])
	nout := os.NewFile(uintptr(oup[0]), "po")

	erp := make([]int, 2)
	err = syscall.Pipe(erp)
	if err != nil {
		fmt.Printf("pipe error: %s", err.Error())
		return
	}
	syscall.SetNonblock(erp[0], true)
	syscall.CloseOnExec(erp[0])
	nerr := os.NewFile(uintptr(erp[0]), "pe")

	njs := exec.Command("node", "runner.js", strconv.Itoa(skp[0]), strconv.Itoa(oup[1]), strconv.Itoa(erp[1]))

	err = njs.Start()
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())
	}

	r := make([]byte, 12)

	d, _ := json.Marshal(map[string]string{"foo": "bar", "xxx": "1"})

	fmt.Printf("1\n")
	sk.Write(d)
	fmt.Printf("1\n")
	sk.Read(r)
	fmt.Printf("1\n")

	fmt.Printf("Ret: [%s]\n", r)
	fmt.Printf("Out:\n")
	readAndOut(nout)
	fmt.Printf("Err:\n")
	readAndOut(nerr)

	d, _ = json.Marshal(map[string]string{"fooz": "baz"})

	fmt.Printf("1\n")
	sk.Write(d)
	fmt.Printf("1\n")
	sk.Read(r)
	fmt.Printf("1\n")

	fmt.Printf("Ret: [%s]\n", r)
	fmt.Printf("Out:\n")
	readAndOut(nout)
	fmt.Printf("Err:\n")
	readAndOut(nerr)

	njs.Process.Kill()
	njs.Wait()
}
