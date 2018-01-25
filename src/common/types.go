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
	DBFuncStateIni	int = 1		// Initializing for add -> Bld/Str
	DBFuncStateStr	int = 2		// Starting -> Rdy
	DBFuncStateRdy	int = 3		// Ready

	DBFuncStateBld	int = 4		// Building -> Str
	DBFuncStateUpd	int = 5		// Update-build -> Rdy

	DBFuncStateTrm	int = 6		// Terminating
	DBFuncStateStl	int = 7		// Stalled
	DBFuncStateDea	int = 8		// Deactivated
)

const (
	SwyPodInstRun string = "run"
	SwyPodInstBld string = "build"
)

const (
	GateGenErr	uint = 1	// Unclassified error
	GateBadRequest	uint = 2	// Error parsing request data
	GateBadResp	uint = 3	// Error generating responce
	GateDbError	uint = 4	// Error requesting database (except NotFound)
	GateDuplicate	uint = 5	// ID duplication
	GateNotFound	uint = 6	// No resource found
	GateFsError	uint = 7	// Error accessing file(s)
	GateWrongType	uint = 8	// Object of wong type
)
