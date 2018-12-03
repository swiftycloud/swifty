BROOT=$(CURDIR)/../../../../

swifty/ruby:
	$(call msg-gen,$@)
	$(Q) $(CP) $(BROOT)/swy-wdog .
	$(Q) $(CP) $(BROOT)/swy-runner .
	$(Q) $(CP) $(BROOT)/src/wdog/runner/runner.rb .
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/ruby

all: swifty/ruby
	@true
.PHONY: all
