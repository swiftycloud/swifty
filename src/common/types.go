package swy

type YAMLConfDB struct {
	StateDB		string		`yaml:"state"`
	Addr		string		`yaml:"address"`
	User		string		`yaml:"user"`
	Pass		string		`yaml:"password"`
}

const (
	DBMwareStateRdy	int = 1		// Ready
	DBMwareStateBsy	int = 2		// Busy
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
)
