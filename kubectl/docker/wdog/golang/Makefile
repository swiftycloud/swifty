BROOT=$(CURDIR)/../../../../
include $(BROOT)/Makefile.inc

layer.tar: .FORCE
	$(call msg-gen,$@)
	$(Q) $(MKDIR) .layer
	$(Q) $(INST) -D $(BROOT)/swy-wdog -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/swy-runner -t .layer/usr/bin/
	$(Q) $(INST) $(BROOT)/go-sca .layer/usr/bin/
	$(Q) $(INST) -m 0644 -D $(BROOT)/src/wdog/runner/runner.go -t .layer/go/src/swyrunner/
	$(Q) $(INST) -m 0644 -T $(BROOT)/src/wdog/runner/body.go .layer/go/src/swyrunner/body
	$(Q) $(INST) -m 0644 -D $(BROOT)/src/wdog/lib/lib.go -t .layer/go/src/swifty/
	$(Q) $(INST) -m 0644 -D $(BROOT)/src/common/xqueue/queue.go -t .layer/go/src/xqueue/
	$(Q) $(INST) runner-go.sh .layer/usr/bin/start_runner.sh
	$(Q) $(INST) builder.sh .layer/usr/bin/build_runner.sh
	$(Q) $(TAR) cf layer.tar --xform='s#.layer##' .layer/
	$(Q) $(RM) -rf .layer

swifty/golang: layer.tar
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/golang

all: swifty/golang
	@true
.PHONY: all
