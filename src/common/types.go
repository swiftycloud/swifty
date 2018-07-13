package swy

const (
	DBMwareStatePrp	int = 1		// Preparing
	DBMwareStateRdy	int = 2		// Ready
	DBMwareStateTrm	int = 3		// Terminating
	DBMwareStateStl	int = 4		// Stalled (while terminating or cleaning up)

	DBMwareStateNo	int = -1	// Doesn't exists :)
)

const (
	DBFuncStateIni	int = 1		// Initializing for add -> Bld/Str
	DBFuncStateStr	int = 2		// Starting -> Rdy
	DBFuncStateRdy	int = 3		// Ready

	DBFuncStateTrm	int = 6		// Terminating
	DBFuncStateStl	int = 7		// Stalled
	DBFuncStateDea	int = 8		// Deactivated

	DBFuncStateNo	int = -1	// Doesn't exists :)
)

const (
	DBDepStateIni	int = 1
	DBDepStateRdy	int = 2
	DBDepStateStl	int = 3
	DBDepStateTrm	int = 4
)

const (
	DBRepoStateCln	int = 1
	DBRepoStateRem	int = 2
)

const (
	GateGenErr	uint = 1	// Unclassified error
	GateBadRequest	uint = 2	// Error parsing request data
	GateBadResp	uint = 3	// Error generating responce
	GateDbError	uint = 4	// Error requesting database (except NotFound)
	GateDuplicate	uint = 5	// ID duplication
	GateNotFound	uint = 6	// No resource found
	GateFsError	uint = 7	// Error accessing file(s)
	GateNotAvail	uint = 8	// Operation not available on selected object
)
