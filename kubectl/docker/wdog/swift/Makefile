BROOT=$(CURDIR)/../../../../

swifty/swift:
	$(call msg-gen,$@)
	$(Q) $(CP) $(BROOT)/swy-wdog .
	$(Q) $(CP) $(BROOT)/swy-runner .
	$(Q) $(CP) $(BROOT)/src/wdog/runner/runner.swift .
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/swift

all: swifty/swift
	@true
.PHONY: all
