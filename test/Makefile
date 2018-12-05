TESTS =

TESTS += test-fns
TESTS += test-aaas

all: $(TESTS)
	true

test-%: test-%.go
	go build $<
