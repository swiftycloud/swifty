swifty/golang:
	$(call msg-gen,$@)
	$(Q) docker build -t $@ .
.PHONY: swifty/golang

all: swifty/golang
	@true
.PHONY: all
