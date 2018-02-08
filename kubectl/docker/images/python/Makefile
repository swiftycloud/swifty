swifty/python:
	$(call msg-gen,$@)
	$(Q) docker build -t $@ .
	$(Q) docker tag $@ $(GITID)
.PHONY: swifty/python

all: swifty/python
	@true
.PHONY: all
