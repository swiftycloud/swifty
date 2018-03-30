swifty/gate:
	$(call msg-gen,$@)
	$(Q) docker build -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/gate

all: swifty/gate
	@true
.PHONY: all
