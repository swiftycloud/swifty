package swy

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
