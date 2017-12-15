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

XELATEX		?= xelatex
XELATEX-OPTS	?= -output-driver="xdvipdfmx -V 3" -8bit

tex-d		+= version.tex
tex-d		+= intro.tex
tex-d		+= setup.tex
tex-d		+= deploy.tex
tex-y		+= swifty.tex

$(tex-y): $(tex-d)
	@true

swifty.pdf: $(tex-y) .FORCE
	$(call msg-gen,$@)
	$(Q) $(XELATEX) $(XELATEX-OPTS) $^
	$(Q) $(XELATEX) $(XELATEX-OPTS) $^
all-y += swifty.pdf

# Default target
all: $(all-y)

clean:
	$(call msg-clean,docs)
	$(Q) $(RM) ./*.aux ./*.idx ./*.ilg ./*.ind ./*.log
	$(Q) $(RM) ./*.out ./*.pdf ./*.toc
.PHONY: clean

.SUFFIXES:
