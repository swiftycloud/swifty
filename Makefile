.PHONY: all .FORCE
.DEFAULT_GOAL := all

ifeq ($(strip $(V)),)
        E := @echo
        Q := @
else
        E := @\#
        Q :=
endif

export E Q

define msg-gen
        $(E) "  GEN     " $(1)
endef

define msg-clean
        $(E) "  CLEAN   " $(1)
endef

export msg-gen msg-clean

RM		?= rm -f
MAKE		?= make
GO		?= go
GO-BUILD-OPTS	?= build

export RM MAKE

go-y		+= src/main.go

vzfaas: $(go-y) .FORCE
	$(call msg-gen,$@)
	$(Q) $(GO) $(GO-BUILD-OPTS) -o $@ $(go-y)
all-y += vzfaas

# Default target
all: $(all-y)

clean:
	$(call msg-clean,vzfaas)
	$(Q) $(RM) vzfaas
.PHONY: clean

.SUFFIXES:
