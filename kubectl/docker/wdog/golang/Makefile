BROOT=$(CURDIR)/../../../../

swifty/golang:
	$(call msg-gen,$@)
	$(Q) $(CP) $(BROOT)/swy-wdog .
	$(Q) $(CP) $(BROOT)/swy-runner .
	$(Q) $(CP) $(BROOT)/src/wdog/runner/runner.go .
	$(Q) $(CP) $(BROOT)/src/wdog/runner/body.go .
	$(Q) $(CP) $(BROOT)/src/wdog/lib/lib.go .
	$(Q) $(CP) $(BROOT)/src/common/xqueue/queue.go .
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/golang

all: swifty/golang
	@true
.PHONY: all
