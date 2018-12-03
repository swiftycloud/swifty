BROOT=$(CURDIR)/../../../../

swifty/csharp:
	$(call msg-gen,$@)
	$(Q) $(CP) $(BROOT)/swy-wdog .
	$(Q) $(CP) $(BROOT)/swy-runner .
	$(Q) $(CP) $(BROOT)/src/wdog/runner/runner.cs .
	$(Q) $(CP) $(BROOT)/src/wdog/lib/XStream.dll .
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/csharp

all: swifty/csharp
	@true
.PHONY: all
