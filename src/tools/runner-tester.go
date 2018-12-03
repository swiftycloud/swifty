package main

import (
	"syscall"
	"os"
	"fmt"
	"time"
	"os/exec"
	"strconv"
	"swifty/common/xqueue"
)

func readLines(f *os.File) string {
	var ret string

	buf := make([]byte, 512, 512)
	for {
		n, _ := f.Read(buf)
		if n == 0 {
			return ret
		}
		ret += string(buf[:n])
	}
}

type RunnerRes struct {
	Res     int
	Status  int
	Ret     string
}

func main() {
	q, err := xqueue.MakeQueue()
	if err != nil {
		fmt.Printf("xqueue error: %s", err.Error())
		return
	}

	oup := make([]int, 2)
	err = syscall.Pipe(oup)
	if err != nil {
		fmt.Printf("pipe error: %s", err.Error())
		return
	}
	syscall.SetNonblock(oup[0], true)
	syscall.CloseOnExec(oup[0])
	outf := os.NewFile(uintptr(oup[0]), "runner.stdout")

	erp := make([]int, 2)
	err = syscall.Pipe(erp)
	if err != nil {
		fmt.Printf("pipe error: %s", err.Error())
		return
	}
	syscall.SetNonblock(erp[0], true)
	syscall.CloseOnExec(erp[0])
	errf := os.NewFile(uintptr(erp[0]), "runner.stdout")

	njs := exec.Command("/usr/bin/swy-runner", strconv.Itoa(oup[1]), strconv.Itoa(erp[1]), q.GetId(), os.Args[1], os.Args[2])

	fmt.Printf("Starting command\n")
	err = njs.Start()
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())
		return
	}

	q.Started()
	go func() {
		for {
			x := readLines(outf)
			if x != "" {
				fmt.Printf("!!![%s]\n", x)
			}
			x = readLines(errf)
			if x != "" {
				fmt.Printf("!!![%s]\n", x)
			}
		}
	}()

	fmt.Printf("Running tests\n")
	n := time.Now()
	var d time.Duration
	for i := 0; i < 20; i++ {
		data := map[string]interface{} {
			"args": map[string]string {
				"name": "foo",
			},
		}
		var ret RunnerRes

		n2 := time.Now()
		fmt.Printf(">%d\n", i)
		err = q.Send(data)
		if err != nil {
			fmt.Printf("error sending: %s\n", err.Error())
			goto out
		}

		fmt.Printf("<%d\n", i)
		err = q.Recv(&ret)
		if err != nil {
			fmt.Printf("error recv: %s\n", err.Error())
			goto out
		}

		fmt.Printf("chk\n");
		x := readLines(outf)
		if x != "" {
			fmt.Printf("[%s]\n", x)
		}
		x = readLines(errf)
		if x != "" {
			fmt.Printf("[%s]\n", x)
		}
		fmt.Printf("%v (%d)\n", ret, time.Since(n2))
	}
	d = time.Since(n)
	fmt.Printf("%d nsec\n", d)
	fmt.Printf("%d each\n", d/20.)

out:
	time.Sleep(time.Second)
	njs.Process.Kill()
	njs.Wait()
}
