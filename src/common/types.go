package swy

const (
	DBMwareStatePrp	int = 1		// Preparing
	DBMwareStateRdy	int = 2		// Ready
	DBMwareStateTrm	int = 3		// Terminating
	DBMwareStateStl	int = 4		// Stalled (while terminating or cleaning up)
)

const (
	DBPodStateNak	int = 0		// No state
	DBPodStateQue	int = 1		// Queued
	DBPodStateRdy	int = 2		// Ready
	DBPodStateTrm	int = 4		// Terminating
	DBPodStateBsy	int = 8		// Busy
)

const (
	DBFuncStateQue	int = 1		// Queued
	DBFuncStateStl	int = 2		// Stalled
	DBFuncStateBld	int = 3		// Building
	DBFuncStateBlt	int = 4		// Built
	DBFuncStatePrt	int = 5		// Partial
	DBFuncStateRdy	int = 6		// Ready
	DBFuncStateUpd	int = 7		// Update-build
	DBFuncStateTrm	int = 8		// Terminating
	DBFuncStateDea	int = 9		// Deactivated
)

const (
	SwyPodInstRun string = "run"
	SwyPodInstBld string = "build"
)

