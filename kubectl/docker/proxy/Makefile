swifty/proxy:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/proxy

all: swifty/proxy
	@true
.PHONY: all
