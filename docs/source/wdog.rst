Watchdog
********

It's possible to test watchdog without wrapping it into container.

Python
======

Start watchdog with local IP, some port and timeout, e.g.

.. code-block:: sh

        SWD_FUNCTION_DESC='{"timeout": 1000, "podtoken": "a"}' SWD_POD_IP='192.168.122.197' SWD_PORT='8686' python3 src/wdog/main.py

In another terminal curl it with data, e.g.

.. code-block:: sh

        curl -i 192.168.122.197:8686/v1/run -d'{"args":{...},"podtoken":"a"}'

Golang
======

Build watchdog

.. code-block:: sh

        make swy-wdog-go

Build runner. For this some preparations should be done

.. code-block:: sh

        cp src/common/xqueue/queue.go $(GOPATH)/src/xqueue/
        cp code_with_fn.go $(GOPATH)/src/swycode/
        go build src/wdog/runner.go
        cp runner /go/src/swycode/function

Start wdog

.. code-block:: sh

        SWD_FUNCTION_DESC='{"timeout": 1000, "podtoken": "a"}' SWD_POD_IP='192.168.122.197' SWD_PORT='8686' ./swy-wdog-go

Then curl it with data
