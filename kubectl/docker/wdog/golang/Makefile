swifty/golang:
	$(call msg-gen,$@)
	$(Q) docker build --network=host -t $@ .
	$(Q) docker tag $@ $@:$(GITID)
.PHONY: swifty/golang

all: swifty/golang
	@true
.PHONY: all
